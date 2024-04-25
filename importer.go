package gofpdi

import (
	"fmt"
	"io"
)

// The Importer class to be used by a pdf generation library
type Importer struct {
	sourceFile    string
	readers       map[string]*PdfReader
	writers       map[string]*PdfWriter
	tplMap        map[int]*TplInfo
	tplN          int
	writer        *PdfWriter
	importedPages map[string]int
}

type TplInfo struct {
	SourceFile string
	Writer     *PdfWriter
	TemplateId int
}

func (im *Importer) GetReader() *PdfReader {
	return im.GetReaderForFile(im.sourceFile)
}

func (im *Importer) GetWriter() *PdfWriter {
	return im.GetWriterForFile(im.sourceFile)
}

func (im *Importer) GetReaderForFile(file string) *PdfReader {
	if _, ok := im.readers[file]; ok {
		return im.readers[file]
	}

	return nil
}

func (im *Importer) GetWriterForFile(file string) *PdfWriter {
	if _, ok := im.writers[file]; ok {
		return im.writers[file]
	}

	return nil
}

func NewImporter() *Importer {
	importer := &Importer{}
	importer.init()

	return importer
}

func (im *Importer) init() {
	im.readers = make(map[string]*PdfReader, 0)
	im.writers = make(map[string]*PdfWriter, 0)
	im.tplMap = make(map[int]*TplInfo, 0)
	im.writer, _ = NewPdfWriter("")
	im.importedPages = make(map[string]int, 0)
}

func (im *Importer) SetSourceFile(f string) {
	im.sourceFile = f

	// If reader hasn't been instantiated, do that now
	if _, ok := im.readers[im.sourceFile]; !ok {
		reader, err := NewPdfReader(im.sourceFile)
		if err != nil {
			panic(err)
		}
		im.readers[im.sourceFile] = reader
	}

	// If writer hasn't been instantiated, do that now
	if _, ok := im.writers[im.sourceFile]; !ok {
		writer, err := NewPdfWriter("")
		if err != nil {
			panic(err)
		}

		// Make the next writer start template numbers at im.tplN
		writer.SetTplIdOffset(im.tplN)
		im.writers[im.sourceFile] = writer
	}
}

func (im *Importer) SetSourceStream(rs io.ReadSeeker) {
	im.sourceFile = fmt.Sprintf("%v", rs)

	if _, ok := im.readers[im.sourceFile]; !ok {
		reader, err := NewPdfReaderFromStream(im.sourceFile, rs)
		if err != nil {
			panic(err)
		}
		im.readers[im.sourceFile] = reader
	}

	// If writer hasn't been instantiated, do that now
	if _, ok := im.writers[im.sourceFile]; !ok {
		writer, err := NewPdfWriter("")
		if err != nil {
			panic(err)
		}

		// Make the next writer start template numbers at im.tplN
		writer.SetTplIdOffset(im.tplN)
		im.writers[im.sourceFile] = writer
	}
}

func (im *Importer) GetNumPages() int {
	result, err := im.GetReader().getNumPages()

	if err != nil {
		panic(err)
	}

	return result
}

func (im *Importer) GetPageSizes() map[int]map[string]map[string]float64 {
	result, err := im.GetReader().getAllPageBoxes(1.0)

	if err != nil {
		panic(err)
	}

	return result
}

func (im *Importer) ImportPage(pageno int, box string) int {
	// If page has already been imported, return existing tplN
	pageNameNumber := fmt.Sprintf("%s-%04d", im.sourceFile, pageno)
	if _, ok := im.importedPages[pageNameNumber]; ok {
		return im.importedPages[pageNameNumber]
	}

	res, err := im.GetWriter().ImportPage(im.GetReader(), pageno, box)
	if err != nil {
		panic(err)
	}

	// Get current template id
	tplN := im.tplN

	// Set tpl info
	im.tplMap[tplN] = &TplInfo{SourceFile: im.sourceFile, TemplateId: res, Writer: im.GetWriter()}

	// Increment template id
	im.tplN++

	// Cache imported page tplN
	im.importedPages[pageNameNumber] = tplN

	return tplN
}

func (im *Importer) SetNextObjectID(objId int) {
	im.GetWriter().SetNextObjectID(objId)
}

// Put form xobjects and get back a map of template names (e.g. /GOFPDITPL1) and their object ids (int)
func (im *Importer) PutFormXobjects() map[string]int {
	res := make(map[string]int, 0)
	tplNamesIds, err := im.GetWriter().PutFormXobjects(im.GetReader())
	if err != nil {
		panic(err)
	}
	for tplName, pdfObjId := range tplNamesIds {
		res[tplName] = pdfObjId.id
	}
	return res
}

// Put form xobjects and get back a map of template names (e.g. /GOFPDITPL1) and their object ids (sha1 hash)
func (im *Importer) PutFormXobjectsUnordered() map[string]string {
	im.GetWriter().SetUseHash(true)
	res := make(map[string]string, 0)
	tplNamesIds, err := im.GetWriter().PutFormXobjects(im.GetReader())
	if err != nil {
		panic(err)
	}
	for tplName, pdfObjId := range tplNamesIds {
		res[tplName] = pdfObjId.hash
	}
	return res
}

// Get object ids (int) and their contents (string)
func (im *Importer) GetImportedObjects() map[int]string {
	res := make(map[int]string, 0)
	pdfObjIdBytes := im.GetWriter().GetImportedObjects()
	for pdfObjId, bytes := range pdfObjIdBytes {
		res[pdfObjId.id] = string(bytes)
	}
	return res
}

// Get object ids (sha1 hash) and their contents ([]byte)
// The contents may have references to other object hashes which will need to be replaced by the pdf generator library
// The positions of the hashes (sha1 - 40 characters) can be obtained by calling GetImportedObjHashPos()
func (im *Importer) GetImportedObjectsUnordered() map[string][]byte {
	res := make(map[string][]byte, 0)
	pdfObjIdBytes := im.GetWriter().GetImportedObjects()
	for pdfObjId, bytes := range pdfObjIdBytes {
		res[pdfObjId.hash] = bytes
	}
	return res
}

// Get the positions of the hashes (sha1 - 40 characters) within each object, to be replaced with
// actual objects ids by the pdf generator library
func (im *Importer) GetImportedObjHashPos() map[string]map[int]string {
	res := make(map[string]map[int]string, 0)
	pdfObjIdPosHash := im.GetWriter().GetImportedObjHashPos()
	for pdfObjId, posHashMap := range pdfObjIdPosHash {
		res[pdfObjId.hash] = posHashMap
	}
	return res
}

// For a given template id (returned from ImportPage), get the template name (e.g. /GOFPDITPL1) and
// the 4 float64 values necessary to draw the template a x,y for a given width and height.
func (im *Importer) UseTemplate(tplid int, _x float64, _y float64, _w float64, _h float64) (string, float64, float64, float64, float64) {
	// Look up template id in importer tpl map
	tplInfo := im.tplMap[tplid]
	return tplInfo.Writer.UseTemplate(tplInfo.TemplateId, _x, _y, _w, _h)
}
