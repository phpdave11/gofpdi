package gofpdi

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

type PdfReader struct {
	availableBoxes []string
	stack          []string
	trailer        *PdfValue
	catalog        *PdfValue
	pages          []*PdfValue
	xrefPos        int
	xref           []map[int]int
	f              *os.File
}

func NewPdfReader(filename string) *PdfReader {
	var err error
	f, err := os.Open(filename)
	if err != nil {
		panic(err)
	}

	parser := &PdfReader{}
	parser.init()
	parser.f = f
	parser.read()
	return parser
}

func (this *PdfReader) init() {
	this.availableBoxes = []string{"/MediaBox", "/CropBox", "/BleedBox", "/TrimBox", "/ArtBox"}
}

type PdfValue struct {
	Type       int
	String     string
	Token      string
	Int        int
	Real       float64
	Bool       bool
	Dictionary map[string]*PdfValue
	Array      []*PdfValue
	Id         int
	NewId      int
	Gen        int
	Value      *PdfValue
	Stream     *PdfValue
	Bytes      []byte
}

// Jump over comments
func (this *PdfReader) skipComments(r *bufio.Reader) {
	var err error
	var b byte

	for {
		b, err = r.ReadByte()
		if err != nil {
			panic(err)
		}

		if b == '\n' || b == '\r' {
			if b == '\r' {
				// Peek and see if next char is \n
				b2, err := r.ReadByte()
				if err != nil {
					panic(err)
				}
				if b2 != '\n' {
					r.UnreadByte()
				}
			}
			break
		} else {
			//fmt.Printf("%s", string(b))
		}
	}
}

// Advance reader so that whitespace is ignored
func (this *PdfReader) skipWhitespace(r *bufio.Reader) {
	var err error
	var b byte

	for {
		b, err = r.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}

		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		} else {
			r.UnreadByte()
			break
		}
	}
}

// Read a token
func (this *PdfReader) readToken(r *bufio.Reader) string {
	// If there is a token available on the stack, pop it out and return it.
	if len(this.stack) > 0 {
		var popped string
		popped, this.stack = this.stack[len(this.stack)-1], this.stack[:len(this.stack)-1]
		return popped
	}

	this.skipWhitespace(r)

	b, err := r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return ""
		}
		panic(err)
	}

	switch b {
	case '[', ']', '(', ')':
		// This is either an array or literal string delimeter, return it.
		return string(b)

	case '<', '>':
		// This could either be a hex string or a dictionary delimiter.
		// Determine the appropriate case and return the token.
		nb, err := r.ReadByte()
		if err != nil {
			panic(err)
		}
		if nb == b {
			return string(b) + string(nb)
		} else {
			r.UnreadByte()
			return string(b)
		}

	case '%':
		this.skipComments(r)
		return this.readToken(r)

	default:
		// FIXME this may not be performant to create new strings for each byte
		// Is it probably better to create a buffer and then convert to a string at the end.
		str := string(b)

	loop:
		for {
			b, err := r.ReadByte()
			if err != nil {
				panic(err)
			}
			switch b {
			case ' ', '%', '[', ']', '<', '>', '(', ')', '\r', '\n', '\t', '/':
				r.UnreadByte()
				break loop
			default:
				str += string(b)
			}
		}
		return str
	}

	return ""
}

// Read a value based on a token
func (this *PdfReader) readValue(r *bufio.Reader, t string) *PdfValue {
	result := &PdfValue{}
	result.Type = -1
	result.Token = t
	result.Dictionary = make(map[string]*PdfValue, 0)
	result.Array = make([]*PdfValue, 0)

	switch t {
	case "<":
		// This is a hex string

		// Read bytes until '>' is found
		var s string
		for {
			b, err := r.ReadByte()
			if err != nil {
				panic(err)
			}
			if b != '>' {
				s += string(b)
			} else {
				break
			}
		}

		result.Type = PDF_TYPE_HEX
		result.String = s

	case "<<":
		// This is a dictionary

		// Recurse into this function until we reach the end of the dictionary.
		for {
			key := this.readToken(r)

			if key == "" {
				panic("empty key")
				return result
			}

			if key == ">>" {
				break
			}

			// read next token
			newKey := this.readToken(r)

			value := this.readValue(r, newKey)
			if value.Type == -1 {
				return result
			}

			// Catch missing value
			if value.Type == PDF_TYPE_TOKEN && value.String == ">>" {
				result.Type = PDF_TYPE_NULL
				result.Dictionary[key] = value
				break
			}

			// Set value in dictionary
			result.Dictionary[key] = value
		}

		result.Type = PDF_TYPE_DICTIONARY
		return result

	case "[":
		// This is an array

		tmpResult := make([]*PdfValue, 0)

		// Recurse into this function until we reach the end of the array
		for {
			key := this.readToken(r)

			if key == "" {
				panic("empty key")
				return result
			}

			if key == "]" {
				break
			}

			value := this.readValue(r, key)
			if value.Type == -1 {
				return result
			}

			tmpResult = append(tmpResult, value)
		}

		result.Type = PDF_TYPE_ARRAY
		result.Array = tmpResult

	case "(":
		// This is a string

		openBrackets := 1
		var s string

		// Read bytes until brackets are balanced
		for openBrackets > 0 {
			b, err := r.ReadByte()
			if err != nil {
				panic(err)
			}
			switch b {
			case '(':
				openBrackets++

			case ')':
				openBrackets--
			}

			if openBrackets > 0 {
				s += string(b)
			}
		}

		result.Type = PDF_TYPE_STRING
		result.String = s

	case "stream":
		panic("to be implemented")

	default:
		result.Type = PDF_TYPE_TOKEN
		result.Token = t

		if is_numeric(t) {
			// A numeric token.  Make sure that it is not part of something else
			t2 := this.readToken(r)
			if t2 != "" {
				if is_numeric(t2) {
					// Two numeric tokens in a row.
					// In this case, we're probably in front of either an object reference
					// or an object specification.
					// Determine the case and return the data.
					t3 := this.readToken(r)
					if t3 != "" {
						switch t3 {
						case "obj":
							result.Type = PDF_TYPE_OBJDEC
							result.Id, _ = strconv.Atoi(t)
							result.Gen, _ = strconv.Atoi(t2)
							return result

						case "R":
							result.Type = PDF_TYPE_OBJREF
							result.Id, _ = strconv.Atoi(t)
							result.Gen, _ = strconv.Atoi(t2)
							return result
						}

						// If we get to this point, that numeric value up there was just a numeric value.
						// Push the extra tokens back into the stack and return the value.
						this.stack = append(this.stack, t3)
					}
				}

				this.stack = append(this.stack, t2)
			}

			if n, err := strconv.Atoi(t); err == nil {
				result.Type = PDF_TYPE_NUMERIC
				result.Int = n
			} else {
				result.Type = PDF_TYPE_REAL
				result.Real, _ = strconv.ParseFloat(t, 64)
			}
		} else if t == "true" || t == "false" {
			result.Type = PDF_TYPE_BOOLEAN
			result.Bool = t == "true"
		} else if t == "null" {
			result.Type = PDF_TYPE_NULL
		} else {
			result.Type = PDF_TYPE_TOKEN
			result.Token = t
		}
	}

	return result
}

func (this *PdfReader) resolveObject(objSpec *PdfValue) *PdfValue {
	// Create new bufio.Reader
	r := bufio.NewReader(this.f)

	if objSpec.Type == PDF_TYPE_OBJREF {
		// This is a reference, resolve it.
		offset := this.xref[objSpec.Id][objSpec.Gen]

		// Save current file position
		// This is needed if you want to resolve reference while you're reading another object.
		// (e.g.: if you need to determine the length of a stream)
		old_pos, err := this.f.Seek(0, os.SEEK_CUR)
		if err != nil {
			panic(err)
		}

		// Reposition the file pointer and load the object header
		this.f.Seek(int64(offset), 0)

		token := this.readToken(r)

		obj := this.readValue(r, token)
		if obj.Type != PDF_TYPE_OBJDEC {
			panic("expected type to be PDF_TYPE_OBJDEC")
		}

		if obj.Id != objSpec.Id {
			panic(fmt.Sprintf("Object ID (%d) does not match ObjSpec ID (%d)", obj.Id, objSpec.Id))
		}

		if obj.Gen != objSpec.Gen {
			panic("Object Gen does not match ObjSpec Gen")
		}

		// Read next token
		token = this.readToken(r)

		// Read actual object value
		value := this.readValue(r, token)

		// Read next token
		token = this.readToken(r)

		result := &PdfValue{}
		result.Id = obj.Id
		result.Gen = obj.Gen
		result.Type = PDF_TYPE_OBJECT
		result.Value = value

		if token == "stream" {
			result.Type = PDF_TYPE_STREAM

			this.skipWhitespace(r)

			// Read stream data
			length := value.Dictionary["/Length"].Int

			// Read length bytes
			bytes := make([]byte, length)

			// Cannot use reader.Read() because that may not read all the bytes
			_, err := io.ReadFull(r, bytes)
			if err != nil {
				panic(err)
			}

			token = this.readToken(r)
			if token != "endstream" {
				panic("Expected next token to be: endstream, got: " + token)
			}

			token = this.readToken(r)

			streamObj := &PdfValue{}
			streamObj.Type = PDF_TYPE_STREAM
			streamObj.Bytes = bytes

			result.Stream = streamObj
		}

		if token != "endobj" {
			panic("Expected next token to be: endobj, got: " + token)
		}

		// Reposition the file pointer to previous position
		this.f.Seek(old_pos, 0)

		return result

	} else {
		return objSpec
	}

	return &PdfValue{}
}

// Find the xref offset (should be at the end of the PDF)
func (this *PdfReader) findXref() {
	var result int
	var err error
	var toRead int64

	toRead = 1500

	// 0 means relative to the origin of the file,
	// 1 means relative to the current offset,
	// and 2 means relative to the end.
	whence := 2

	// Perform seek operation
	this.f.Seek(-toRead, whence)

	// Allocate data
	var data []byte
	data = make([]byte, toRead)

	// Read []byte into data
	_, err = this.f.Read(data)
	if err != nil {
		panic(err)
	}

	foundStartXref := false
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if foundStartXref {
			// Convert line (string) into int
			result, err = strconv.Atoi(line)
			if err != nil {
				panic(err)
			}
			break
		} else if line == "startxref" {
			// The next line will be the value we want to return
			foundStartXref = true
		}
	}

	// Rewind file pointer
	whence = 0
	this.f.Seek(0, whence)

	this.xrefPos = result
}

// Read and parse the xref table
func (this *PdfReader) readXref() {
	// Create new bufio.Reader
	r := bufio.NewReader(this.f)

	// Set file pointer to xref start
	this.f.Seek(int64(this.xrefPos), 0)

	// Xref should start with 'xref'
	t := this.readToken(r)
	if t != "xref" {
		panic("expected xref to start with 'xref'")
	}

	// Next value should always be '0'
	t = this.readToken(r)
	if t != "0" {
		panic("expected next token in xref to be '0'")
	}

	// Determine how many objects there are
	maxObject, err := strconv.Atoi(this.readToken(r))
	if err != nil {
		panic(err)
	}

	// Create a slice of map[int]int with capacity of maxObject
	data := make([]map[int]int, maxObject)

	// For all objects in xref, read object position, object generation, and status (free or new)
	for i := 0; i < maxObject; i++ {
		// Get object position as int
		objPos, err := strconv.Atoi(this.readToken(r))
		if err != nil {
			panic(err)
		}

		// Get object generation as int
		objGen, err := strconv.Atoi(this.readToken(r))
		if err != nil {
			panic(err)
		}

		// Get object status (free or new)
		objStatus := this.readToken(r)
		if objStatus != "f" && objStatus != "n" {
			panic("expected objStatus to be 'n' or 'f', got: " + objStatus)
		}

		// Allocate map[int]int
		data[i] = make(map[int]int, 1)

		// Set object id, generation, and position
		data[i][objGen] = objPos
	}

	// Next, parse trailer
	t = this.readToken(r)
	if t != "trailer" {
		panic("expected next token in xref to be 'trailer', got: " + t)
	}

	t = this.readToken(r)

	// Read trailer dictionary
	this.trailer = this.readValue(r, t)

	// set xref table
	this.xref = data
}

// Read root (catalog object)
func (this *PdfReader) readRoot() {
	rootObjSpec := this.trailer.Dictionary["/Root"]

	// Read root (catalog)
	this.catalog = this.resolveObject(rootObjSpec)
}

// Read all pages in PDF
func (this *PdfReader) readPages() {
	// resolve_pages_dict
	pagesDict := this.resolveObject(this.catalog.Value.Dictionary["/Pages"])

	// This will normally return itself
	kids := this.resolveObject(pagesDict.Value.Dictionary["/Kids"])

	// Allocate pages
	this.pages = make([]*PdfValue, len(kids.Array))

	// Loop through pages and add to result
	for i := 0; i < len(kids.Array); i++ {
		page := this.resolveObject(kids.Array[i])

		// Set page
		this.pages[i] = page
	}
}

// Get references to page resources for a given page number
func (this *PdfReader) getPageResources(pageno int) *PdfValue {
	// TODO:  Add multi page support
	if pageno != 1 {
		panic("at the disco")
	}

	// TODO:  Add multi page support
	if len(this.pages) != 1 {
		panic("at the disco")
	}

	// Resolve page object
	page := this.resolveObject(this.pages[0])

	// Check to see if /Resources exists in Dictionary
	if _, ok := page.Value.Dictionary["/Resources"]; ok {
		// Resolve /Resources object
		res := this.resolveObject(page.Value.Dictionary["/Resources"])

		// If type is PDF_TYPE_OBJECT, return its Value
		if res.Type == PDF_TYPE_OBJECT {
			return res.Value
		}

		// Otherwise, returned the resolved object
		return res
	} else {
		// If /Resources does not exist, check to see if /Parent exists and return that
		if _, ok := page.Value.Dictionary["/Parent"]; ok {
			// Resolve parent object
			res := this.resolveObject(page.Value.Dictionary["/Parent"])

			// If /Parent object type is PDF_TYPE_OBJECT, return its Value
			if res.Type == PDF_TYPE_OBJECT {
				return res.Value
			}

			// Otherwise, return the resolved parent object
			return res
		}
	}

	// Return an empty PdfValue if we got here
	// TODO:  Improve error handling
	return &PdfValue{}
}

// Get page content and return a slice of PdfValue objects
func (this *PdfReader) getPageContent(objSpec *PdfValue) []*PdfValue {
	// Allocate slice
	contents := make([]*PdfValue, 0)

	if objSpec.Type == PDF_TYPE_OBJREF {
		// If objSpec is an object reference, resolve the object and append it to contents
		content := this.resolveObject(objSpec)
		contents = append(contents, content)
	} else if objSpec.Type == PDF_TYPE_ARRAY {
		// If objSpec is an array, loop through the array and recursively get page content and append to contents
		for i := 0; i < len(objSpec.Array); i++ {
			tmpContents := this.getPageContent(objSpec.Array[i])
			for j := 0; j < len(tmpContents); j++ {
				contents = append(contents, tmpContents[j])
			}
		}
	}

	return contents
}

// Get content (i.e. PDF drawing instructions)
func (this *PdfReader) getContent(pageno int) string {
	// TODO:  Add multi page support
	if pageno != 1 {
		panic("at the disco")
	}

	// TODO:  Add multi page support
	if len(this.pages) != 1 {
		panic("at the disco")
	}

	// TODO:  Add multi page support
	page := this.pages[0]

	// FIXME: This could be slow, converting []byte to string and appending many times
	buffer := ""

	// Check to make sure /Contents exists in page dictionary
	if _, ok := page.Value.Dictionary["/Contents"]; ok {
		// Get an array of page content
		contents := this.getPageContent(page.Value.Dictionary["/Contents"])

		for i := 0; i < len(contents); i++ {
			// Decode content if one or more /Filter is specified.
			// Most common filter is FlateDecode which can be uncompressed with zlib
			tmpBuffer := this.rebuildContentStream(contents[i])

			// FIXME:  This is probably slow
			buffer += string(tmpBuffer)
		}
	}

	return buffer
}

// Rebuild content stream
// This will decode content if one or more /Filter (such as FlateDecode) is specified.
// If there are multiple filters, they will be decoded in the order in which they were specified.
func (this *PdfReader) rebuildContentStream(content *PdfValue) []byte {
	// Allocate slice of PdfValue
	filters := make([]*PdfValue, 0)

	// If content has a /Filter, append it to filters slice
	if _, ok := content.Value.Dictionary["/Filter"]; ok {
		filter := content.Value.Dictionary["/Filter"]

		// If filter type is a reference, resolve it
		if filter.Type == PDF_TYPE_OBJREF {
			tmpFilter := this.resolveObject(filter)
			filter = tmpFilter.Value
		}

		if filter.Type == PDF_TYPE_TOKEN {
			// If filter type is a token (e.g. FlateDecode), appent it to filters slice
			filters = append(filters, filter)
		} else if filter.Type == PDF_TYPE_ARRAY {
			// If filter type is an array, then there are multiple filters.  Set filters variable to array value.
			filters = filter.Array
		}

	}

	// Set stream variable to content bytes
	stream := content.Stream.Bytes

	// Loop through filters and apply each filter to stream
	for i := 0; i < len(filters); i++ {
		switch filters[i].Token {
		case "/FlateDecode":
			// Uncompress zlib compressed data
			var out bytes.Buffer
			zlibReader, _ := zlib.NewReader(bytes.NewBuffer(stream))
			defer zlibReader.Close()
			io.Copy(&out, zlibReader)

			// Set stream to uncompressed data
			stream = out.Bytes()
		default:
			panic("Unspported filter: " + filters[i].Token)
		}
	}

	return stream
}

// Get all page box data
func (this *PdfReader) getPageBoxes(pageno int, k float64) map[string]map[string]float64 {
	// Allocate result with the number of available boxes
	result := make(map[string]map[string]float64, len(this.availableBoxes))

	// Resolve page object
	page := this.resolveObject(this.pages[0])

	// Loop through available boxes and add to result
	for i := 0; i < len(this.availableBoxes); i++ {
		box := this.getPageBox(page, this.availableBoxes[i], k)

		result[this.availableBoxes[i]] = box
	}

	return result
}

// Get a specific page box value (e.g. MediaBox) and return its values
func (this *PdfReader) getPageBox(page *PdfValue, box_index string, k float64) map[string]float64 {
	// Allocate 8 fields in result
	result := make(map[string]float64, 8)

	// Check to make sure box_index (e.g. MediaBox) exists in page dictionary
	if _, ok := page.Value.Dictionary[box_index]; ok {
		box := page.Value.Dictionary[box_index]

		// If the box type is a reference, resolve it
		if box.Type == PDF_TYPE_OBJREF {
			tmpBox := this.resolveObject(box)
			box = tmpBox.Value
		}

		if box.Type == PDF_TYPE_ARRAY {
			// If the box type is an array, calculate scaled value based on k
			result["x"] = box.Array[0].Real / k
			result["y"] = box.Array[1].Real / k
			result["w"] = math.Abs(box.Array[0].Real-box.Array[2].Real) / k
			result["h"] = math.Abs(box.Array[1].Real-box.Array[3].Real) / k
			result["llx"] = math.Min(box.Array[0].Real, box.Array[2].Real)
			result["lly"] = math.Min(box.Array[1].Real, box.Array[3].Real)
			result["urx"] = math.Max(box.Array[0].Real, box.Array[2].Real)
			result["ury"] = math.Max(box.Array[1].Real, box.Array[3].Real)
		} else if _, ok := page.Value.Dictionary["/Parent"]; ok {
			// If the page box is inherited from /Parent, recursively return page box of parent
			return this.getPageBox(this.resolveObject(page.Value.Dictionary["/Parent"]), box_index, k)
		} else {
			// TODO: Improve error handling
			panic("could not get page box, and no parent exists")
		}
	}

	return result
}

// Get page rotation for a page number
func (this *PdfReader) getPageRotation(pageno int) *PdfValue {
	// TODO:  Add multi page support
	if pageno != 1 {
		panic("at the disco")
	}

	// TODO:  Add multi page support
	if len(this.pages) != 1 {
		panic("at the disco")
	}

	return this._getPageRotation(this.pages[pageno-1])
}

// Get page rotation for a page object spec
func (this *PdfReader) _getPageRotation(page *PdfValue) *PdfValue {
	// Resolve page object
	page = this.resolveObject(page)

	// Check to make sure /Rotate exists in page dictionary
	if _, ok := page.Value.Dictionary["/Rotate"]; ok {
		res := this.resolveObject(page.Value.Dictionary["/Rotate"])

		// If the type is PDF_TYPE_OBJECT, return its value
		if res.Type == PDF_TYPE_OBJECT {
			return res.Value
		}

		// Otherwise, return the object
		return res
	} else {
		if _, ok := page.Value.Dictionary["/Parent"]; !ok {
			// If we got here and page does not have a /Parent, that is an error
			panic("no parent")
		} else {
			// Recursively return /Parent page rotation
			res := this._getPageRotation(page.Value.Dictionary["/Parent"])

			// If the type is PDF_TYPE_OBJECT, return its value
			if res.Type == PDF_TYPE_OBJECT {
				return res.Value
			}

			// Otherwise, return the object
			return res
		}
	}

	return &PdfValue{}
}

func (this *PdfReader) read() {
	// Find xref position
	this.findXref()

	// Parse xref table
	this.readXref()

	// Read catalog
	this.readRoot()

	// Read pages
	this.readPages()
}
