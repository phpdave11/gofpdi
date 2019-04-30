package gofpdi

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math"
	"os"
)

type PdfWriter struct {
	f *os.File
	k float64
	tpls []*PdfTemplate
	n int
	offsets map[int]int
	offset int
	// Keep track of which objects have already been written
	obj_stack map[int]*PdfValue
	don_obj_stack map[int]*PdfValue
}

func (this *PdfWriter) Init() {
	this.k = 1
	this.offsets = make(map[int]int, 0)
	this.obj_stack = make(map[int]*PdfValue, 0)
	this.don_obj_stack = make(map[int]*PdfValue, 0)
	this.tpls = make([]*PdfTemplate, 1)
}

func NewPdfWriter(filename string) *PdfWriter {
    var err error
    //f, err := os.Open(filename)
    if err != nil {
        panic(err)
    }

	writer := &PdfWriter{}
	writer.Init()
	//writer.f = f
	return writer
}

// Done with parsing.  Now, create templates.
type PdfTemplate struct {
	Reader    *PdfReader
	Resources *PdfValue
	Buffer    string
	Box       map[string]float64
	Boxes     map[string]map[string]float64
	X         float64
	Y         float64
	W         float64
	H         float64
	Rotation  int
	N         int
}

var k float64

// Create a PdfTemplate object from a page number (e.g. 1) and a boxName (e.g. MediaBox)
func (this *PdfWriter) importPage(reader *PdfReader, pageno int, boxName string) *PdfTemplate {
	// TODO: Improve error handling
/*
	if !in_array(boxName, availableBoxes) {
		panic("box '" + boxName + "' not in available boxes")
	}
*/

	// Set default scale to 1
	k = 1

	// Get all page boxes
	pageBoxes := reader.getPageBoxes(1, k)

	// If requested box name does not exist for this page, use an alternate box
	if _, ok := pageBoxes[boxName]; !ok {
		if boxName == "/BleedBox" || boxName == "/TrimBox" || boxName == "ArtBox" {
			boxName = "/CropBox"
		} else if boxName == "/CropBox" {
			boxName = "/MediaBox"
		}
	}

	// If the requested box name or an alternate box name cannot be found, trigger an error
	// TODO: Improve error handling
	if _, ok := pageBoxes[boxName]; !ok {
		panic("Box not found: " + boxName)
	}

	// Set template values
	tpl := &PdfTemplate{}
	tpl.Reader = reader
	tpl.Resources = reader.getPageResources(pageno)
	tpl.Buffer = reader.getContent(1)
	tpl.Box = pageBoxes[boxName]
	tpl.Boxes = pageBoxes
	tpl.X = 0
	tpl.Y = 0
	tpl.W = tpl.Box["w"]
	tpl.H = tpl.Box["h"]

	// Set template rotation
	rotation := reader.getPageRotation(pageno)
	angle := rotation.Int % 360

	// Normalize angle
	if angle != 0 {
		steps := angle / 90
		w := tpl.W
		h := tpl.H

		if steps%2 == 0 {
			tpl.W = w
			tpl.H = h
		} else {
			tpl.W = h
			tpl.H = w
		}

		if angle < 0 {
			angle += 360
		}

		tpl.Rotation = angle * -1
	}

	this.tpls[0] = tpl

	return tpl
}

// Create a new object and keep track of the offset for the xref table
func (this *PdfWriter) newObj(objId int, onlyNewObj bool) {
	if objId < 0 {
		this.n++
		objId = this.n
	}

	if !onlyNewObj {
		this.offsets[objId] = this.offset
		this.out(fmt.Sprintf("%d 0 obj", objId))
	}
}

// Output PDF data with a newline
func (this *PdfWriter) out(s string) {
	this.offset += len(s)
	fmt.Print(s)

	this.offset++
	fmt.Print("\n")
}

// Output PDF data
func (this *PdfWriter) straightOut(s string) {
	this.offset += len(s)
	fmt.Print(s)
}

// Output a PdfValue
func (this *PdfWriter) writeValue(value *PdfValue) {
	switch value.Type {
	case PDF_TYPE_TOKEN:
		this.straightOut(value.Token + " ")
		break

	case PDF_TYPE_NUMERIC:
		this.straightOut(fmt.Sprintf("%d", value.Int) + " ")
		break

	case PDF_TYPE_REAL:
		this.straightOut(fmt.Sprintf("%F", value.Real) + " ")
		break

	case PDF_TYPE_ARRAY:
		this.straightOut("[")
		for i := 0; i < len(value.Array); i++ {
			this.writeValue(value.Array[i])
		}
		this.out("]")
		break

	case PDF_TYPE_DICTIONARY:
		this.straightOut("<<")
		for k, v := range value.Dictionary {
			this.straightOut(k + " ")
			this.writeValue(v)
		}
		this.straightOut(">>")
		break

	case PDF_TYPE_OBJREF:
		// An indirect object reference.  Fill the object stack if needed.
		// Check to see if object already exists on the don_obj_stack.
		if _, ok := this.don_obj_stack[value.Id]; !ok {
			this.newObj(-1, true)
			this.obj_stack[value.Id] = &PdfValue{Type: PDF_TYPE_OBJREF, Gen: value.Gen, Id: value.Id, NewId: this.n}
			this.don_obj_stack[value.Id] = &PdfValue{Type: PDF_TYPE_OBJREF, Gen: value.Gen, Id: value.Id, NewId: this.n}
		}

		// Get object ID from don_obj_stack
		objId := this.don_obj_stack[value.Id].NewId
		this.out(fmt.Sprintf("%d 0 R", objId))
		break

	case PDF_TYPE_STRING:
		// A string
		this.straightOut("(" + value.String + ")")
		break

	case PDF_TYPE_STREAM:
		// A stream.  First, output the stream dictionary, then the stream data itself.
		this.writeValue(value.Value)
		this.out("stream")
		this.out(string(value.Stream.Bytes))
		this.out("endstream")
		break

	case PDF_TYPE_HEX:
		this.straightOut("<" + value.String + ">")
		break

	case PDF_TYPE_BOOLEAN:
		if value.Bool {
			this.straightOut("true")
		} else {
			this.straightOut("false")
		}
		break

	case PDF_TYPE_NULL:
		// The null object
		this.straightOut("null ")
		break
	}
}

// Output Form XObjects (1 for each template)
func (this *PdfWriter) putFormXobjects(reader *PdfReader) {
	compress := true
	filter := ""
	if compress {
		filter = "/Filter /FlateDecode "
	}

	for i := 0; i < len(this.tpls); i++ {
		tpl := this.tpls[i]

		var p string
		if compress {
			var b bytes.Buffer
			w := zlib.NewWriter(&b)
			w.Write([]byte(tpl.Buffer))
			w.Close()

			p = b.String()
		} else {
			p = tpl.Buffer
		}

		// Create new PDF object
		this.newObj(-1, false)

		cN := this.n // remember current "n"

		tpl.N = this.n

		this.out("<<" + filter + "/Type /XObject")
		this.out("/Subtype /Form")
		this.out("/FormType 1")

		this.out(fmt.Sprintf("/BBox [%.2F %.2F %.2F %.2F]", tpl.Box["llx"]*k, tpl.Box["lly"]*k, (tpl.Box["urx"]+tpl.X)*k, (tpl.Box["ury"]-tpl.Y)*k))

		var c, s, tx, ty float64
		c = 1

		// Handle rotated pages
		if tpl.Box != nil {
			tx = -tpl.Box["llx"]
			ty = -tpl.Box["lly"]

			if tpl.Rotation != 0 {
				angle := float64(tpl.Rotation) * math.Pi / 180.0
				c = math.Cos(float64(angle))
				s = math.Sin(float64(angle))

				switch tpl.Rotation {
				case -90:
					tx = -tpl.Box["lly"]
					ty = tpl.Box["urx"]
					break

				case -180:
					tx = tpl.Box["urx"]
					ty = tpl.Box["ury"]
					break

				case -270:
					tx = tpl.Box["ury"]
					ty = -tpl.Box["llx"]
				}
			}
		} else {
			tx = -tpl.Box["x"] * 2
			ty = tpl.Box["y"] * 2
		}

		tx *= k
		ty *= k

		if c != 1 || s != 0 || tx != 0 || ty != 0 {
			this.out(fmt.Sprintf("/Matrix [%.5F %.5F %.5F %.5F %.5F %.5F]", c, s, -s, c, tx, ty))
		}

		// Now write resources
		this.out("/Resources ")

		if tpl.Resources != nil {
			this.writeValue(tpl.Resources) // "n" will be changed
		} else {
			panic("what to do here?")
		}

		nN := this.n // remember new "n"
		this.n = cN  // reset to current "n"

		this.out("/Length " + fmt.Sprintf("%d", len(p)) + " >>")

		this.out("stream")
		this.out(p)
		this.out("endstream")

		this.n = nN // reset to new "n"

		// Put imported objects, starting with the ones from the XObject's Resources,
		// then from dependencies of those resources).
		this.putImportedObjects(reader)
	}
}

func (this *PdfWriter) putImportedObjects(reader *PdfReader) {
	// obj_stack will have new items added to it in the inner loop, so do another loop to check for extras
	// TODO make the order of this the same every time
	for {
		atLeastOne := false
		for k, v := range this.obj_stack {
			if v == nil {
				continue
			}

			atLeastOne = true

			nObj := reader.resolveObject(v)

			// New object with "NewId" field
			this.newObj(v.NewId, false)

			if nObj.Type == PDF_TYPE_STREAM {
				this.writeValue(nObj)
			} else {
				this.writeValue(nObj.Value)
			}

			this.out("endobj")

			// Remove from stack
			this.obj_stack[k] = nil
		}

		if !atLeastOne {
			break
		}
	}
}

// Get the calculated size of a template
// If one size is given, this method calculates the other one
func (this *PdfWriter) getTemplateSize(tplid int, _w float64, _h float64) map[string]float64 {
	result := make(map[string]float64, 2)

	tpl := this.tpls[0]

	w := tpl.W
	h := tpl.H

	if _w == 0 && _h == 0 {
		_w = w
		_h = h
	}

	if _w == 0 {
		_w = _h * w / h
	}

	if _h == 0 {
		_h = _w * h / w
	}

	result["w"] = _w
	result["h"] = _h

	return result
}

// Generate PDF drawing code to draw a Template (Form XObject) onto a page
func (this *PdfWriter) useTemplate(tpl *PdfTemplate, tplid int, _x float64, _y float64, _w float64, _h float64) string {
	result := ""

	result += "q 0 J 1 w 0 j 0 G 0 g\n" // reset standard values

	//tpl := this.tpls[0]

	w := tpl.W
	h := tpl.H

	_x += tpl.X
	_y += tpl.Y

	wh := this.getTemplateSize(tplid, _w, _h)

	_w = wh["w"]
	_h = wh["h"]

	tData := make(map[string]float64, 9)
	tData["x"] = 0.0
	tData["y"] = 0.0
	tData["w"] = _w
	tData["h"] = _h
	tData["scaleX"] = (_w / w)
	tData["scaleY"] = (_h / h)
	tData["tx"] = _x
	tData["ty"] = (3 - _y - _h)
	tData["lty"] = (3 - _y - _h) - (3 - h) * (_h / h)

	result += fmt.Sprintf("q %.4F 0 0 %.4F %.4F %.4F cm\n", tData["scaleX"], tData["scaleY"], tData["tx"] * k, tData["ty"] * k) // translate
	result += fmt.Sprintf("%s%d Do Q", "/TPL", 1)

	result += "\nQ"

	return result
}

func Demo() {
	// Create new reader
	reader := NewPdfReader("/Users/dave/Desktop/PDFPL110.pdf")

	// Create new writer
	writer := NewPdfWriter("/Users/dave/Desktop/pdfwriter-output.pdf")

	tpl := writer.importPage(reader, 1, "/CropBox")

	writer.out("%PDF-1.4\n%ABCD\n\n")

	writer.putFormXobjects(reader)

	pagesObjId := writer.n + 1
	pageObjId := writer.n + 2
	contentsObjId := writer.n + 3
	catalogObjId := writer.n + 4

	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", pageObjId))
	writer.out("endobj")

	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /LastModified (D:20190412184239+00'00') /Resources %s /MediaBox [0.000000 0.000000 1319.976000 887.976000] /CropBox [0.000000 0.000000 1319.976000 887.976000] /BleedBox [20.988000 20.988000 1298.988000 866.988000] /TrimBox [29.988000 29.988000 1289.988000 857.988000] /Contents %d 0 R /Rotate 0 /Group << /Type /Group /S /Transparency /CS /DeviceRGB >> /PZ 1 >>", pagesObjId, fmt.Sprintf("<</ProcSet [/PDF /Text /ImageB /ImageC /ImageI ] /XObject <</TPL1 %d 0 R>>>>", 1), contentsObjId));
	writer.out("endobj");

	str := writer.useTemplate(tpl, 1, 0.0, -887.976 * 0.5, 1319.976 * 0.5, 0.0)

	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<</Length %d>>\nstream", len(str)))
	writer.out(str)
	writer.out("endstream")
	writer.out("endobj")

	// draw template

	// put catalog
	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObjId));

	// get xref position
	xrefPos := writer.offset

	// put xref
	writer.out("xref")
	numObjects := writer.n + 1

	// write number of objects
	writer.out(fmt.Sprintf("0 %d", numObjects))

	// first object always points to byte 0
	writer.out("0000000000 65535 f ")

	// write object posisions
	for i := 1; i < numObjects; i++ {
		writer.out(fmt.Sprintf("%010d 00000 n ", writer.offsets[i]))
	}

	writer.out("trailer")

	writer.out(fmt.Sprintf("<< /Size %d /Root %d 0 R >>", numObjects, catalogObjId))

	writer.out("startxref")
	writer.out(fmt.Sprintf("%d", xrefPos))
	writer.out("%%EOF")
}
