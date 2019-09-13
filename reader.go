package gofpdi

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"math"
	"os"
	"strconv"
)

type PdfReader struct {
	availableBoxes []string
	stack          []string
	trailer        *PdfValue
	catalog        *PdfValue
	pages          []*PdfValue
	xrefPos        int
	xref           map[int]map[int]int
	f              io.ReadSeeker
	nBytes         int64
	sourceFile     string
}

func NewPdfReaderFromStream(rs io.ReadSeeker) (*PdfReader, error) {
	length, err := rs.Seek(0, 2)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to determine stream length")
	}
	parser := &PdfReader{f: rs, nBytes: length}
	if err := parser.init(); err != nil {
		return nil, errors.Wrap(err, "Failed to initialize parser")
	}
	if err := parser.read(); err != nil {
		return nil, errors.Wrap(err, "Failed to read pdf from stream")
	}
	return parser, nil
}

func NewPdfReader(filename string) (*PdfReader, error) {
	var err error
	f, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to open file")
	}
	info, err := f.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to obtain file information")
	}

	parser := &PdfReader{f: f, sourceFile: filename, nBytes: info.Size()}
	if err = parser.init(); err != nil {
		return nil, errors.Wrap(err, "Failed to initialize parser")
	}
	if err = parser.read(); err != nil {
		return nil, errors.Wrap(err, "Failed to read pdf")
	}

	return parser, nil
}

func (this *PdfReader) init() error {
	this.availableBoxes = []string{"/MediaBox", "/CropBox", "/BleedBox", "/TrimBox", "/ArtBox"}
	this.xref = make(map[int]map[int]int, 0)
	err := this.read()
	if err != nil {
		return errors.Wrap(err, "Failed to read pdf")
	}
	return nil
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
func (this *PdfReader) skipComments(r *bufio.Reader) error {
	var err error
	var b byte

	for {
		b, err = r.ReadByte()
		if err != nil {
			return errors.Wrap(err, "Failed to ReadByte while skipping comments")
		}

		if b == '\n' || b == '\r' {
			if b == '\r' {
				// Peek and see if next char is \n
				b2, err := r.ReadByte()
				if err != nil {
					return errors.Wrap(err, "Failed to read byte")
				}
				if b2 != '\n' {
					r.UnreadByte()
				}
			}
			break
		}
	}

	return nil
}

// Advance reader so that whitespace is ignored
func (this *PdfReader) skipWhitespace(r *bufio.Reader) error {
	var err error
	var b byte

	for {
		b, err = r.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return errors.Wrap(err, "Failed to read byte")
		}

		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		} else {
			r.UnreadByte()
			break
		}
	}

	return nil
}

// Read a token
func (this *PdfReader) readToken(r *bufio.Reader) (string, error) {
	var err error

	// If there is a token available on the stack, pop it out and return it.
	if len(this.stack) > 0 {
		var popped string
		popped, this.stack = this.stack[len(this.stack)-1], this.stack[:len(this.stack)-1]
		return popped, nil
	}

	err = this.skipWhitespace(r)
	if err != nil {
		return "", errors.Wrap(err, "Failed to skip whitespace")
	}

	b, err := r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return "", nil
		}
		return "", errors.Wrap(err, "Failed to read byte")
	}

	switch b {
	case '[', ']', '(', ')':
		// This is either an array or literal string delimeter, return it.
		return string(b), nil

	case '<', '>':
		// This could either be a hex string or a dictionary delimiter.
		// Determine the appropriate case and return the token.
		nb, err := r.ReadByte()
		if err != nil {
			return "", errors.Wrap(err, "Failed to read byte")
		}
		if nb == b {
			return string(b) + string(nb), nil
		} else {
			r.UnreadByte()
			return string(b), nil
		}

	case '%':
		err = this.skipComments(r)
		if err != nil {
			return "", errors.Wrap(err, "Failed to skip comments")
		}
		return this.readToken(r)

	default:
		// FIXME this may not be performant to create new strings for each byte
		// Is it probably better to create a buffer and then convert to a string at the end.
		str := string(b)

	loop:
		for {
			b, err := r.ReadByte()
			if err != nil {
				return "", errors.Wrap(err, "Failed to read byte")
			}
			switch b {
			case ' ', '%', '[', ']', '<', '>', '(', ')', '\r', '\n', '\t', '/':
				r.UnreadByte()
				break loop
			default:
				str += string(b)
			}
		}
		return str, nil
	}

	return "", nil
}

// Read a value based on a token
func (this *PdfReader) readValue(r *bufio.Reader, t string) (*PdfValue, error) {
	var err error
	var b byte

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
			b, err = r.ReadByte()
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read byte")
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
			key, err := this.readToken(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read token")
			}
			if key == "" {
				return nil, errors.New("Token is empty")
			}

			if key == ">>" {
				break
			}

			// read next token
			newKey, err := this.readToken(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read token")
			}

			value, err := this.readValue(r, newKey)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read value for token: "+newKey)
			}

			if value.Type == -1 {
				return result, nil
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
		return result, nil

	case "[":
		// This is an array

		tmpResult := make([]*PdfValue, 0)

		// Recurse into this function until we reach the end of the array
		for {
			key, err := this.readToken(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read token")
			}
			if key == "" {
				return nil, errors.New("Token is empty")
			}

			if key == "]" {
				break
			}

			value, err := this.readValue(r, key)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read value for token: "+key)
			}

			if value.Type == -1 {
				return result, nil
			}

			tmpResult = append(tmpResult, value)
		}

		result.Type = PDF_TYPE_ARRAY
		result.Array = tmpResult

	case "(":
		// This is a string

		openBrackets := 1

		// Create new buffer
		var buf bytes.Buffer

		// Read bytes until brackets are balanced
		for openBrackets > 0 {
			b, err := r.ReadByte()

			if err != nil {
				return nil, errors.Wrap(err, "Failed to read byte")
			}

			switch b {
			case '(':
				openBrackets++

			case ')':
				openBrackets--

			case '\\':
				nb, err := r.ReadByte()
				if err != nil {
					return nil, errors.Wrap(err, "Failed to read byte")
				}

				buf.WriteByte(b)
				buf.WriteByte(nb)

				continue
			}

			if openBrackets > 0 {
				buf.WriteByte(b)
			}
		}

		result.Type = PDF_TYPE_STRING
		result.String = buf.String()

	case "stream":
		return nil, errors.New("Stream not implemented")

	default:
		result.Type = PDF_TYPE_TOKEN
		result.Token = t

		if is_numeric(t) {
			// A numeric token.  Make sure that it is not part of something else
			t2, err := this.readToken(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read token")
			}
			if t2 != "" {
				if is_numeric(t2) {
					// Two numeric tokens in a row.
					// In this case, we're probably in front of either an object reference
					// or an object specification.
					// Determine the case and return the data.
					t3, err := this.readToken(r)
					if err != nil {
						return nil, errors.Wrap(err, "Failed to read token")
					}

					if t3 != "" {
						switch t3 {
						case "obj":
							result.Type = PDF_TYPE_OBJDEC
							result.Id, _ = strconv.Atoi(t)
							result.Gen, _ = strconv.Atoi(t2)
							return result, nil

						case "R":
							result.Type = PDF_TYPE_OBJREF
							result.Id, _ = strconv.Atoi(t)
							result.Gen, _ = strconv.Atoi(t2)
							return result, nil
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
				result.Real = float64(n) // Also assign Real value here to fix page box parsing bugs
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

	return result, nil
}

func (this *PdfReader) resolveObject(objSpec *PdfValue) (*PdfValue, error) {
	var err error
	var old_pos int64

	// Create new bufio.Reader
	r := bufio.NewReader(this.f)

	if objSpec.Type == PDF_TYPE_OBJREF {
		// This is a reference, resolve it.
		offset := this.xref[objSpec.Id][objSpec.Gen]

		// Save current file position
		// This is needed if you want to resolve reference while you're reading another object.
		// (e.g.: if you need to determine the length of a stream)
		old_pos, err = this.f.Seek(0, os.SEEK_CUR)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to get current position of file")
		}

		// Reposition the file pointer and load the object header
		_, err = this.f.Seek(int64(offset), 0)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to set position of file")
		}

		token, err := this.readToken(r)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to read token")
		}

		obj, err := this.readValue(r, token)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to read value for token: "+token)
		}

		if obj.Type != PDF_TYPE_OBJDEC {
			return nil, errors.New(fmt.Sprintf("Expected type to be PDF_TYPE_OBJDEC, got: %d", obj.Type))
		}

		if obj.Id != objSpec.Id {
			return nil, errors.New(fmt.Sprintf("Object ID (%d) does not match ObjSpec ID (%d)", obj.Id, objSpec.Id))
		}

		if obj.Gen != objSpec.Gen {
			return nil, errors.New("Object Gen does not match ObjSpec Gen")
		}

		// Read next token
		token, err = this.readToken(r)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to read token")
		}

		// Read actual object value
		value, err := this.readValue(r, token)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to read value for token: "+token)
		}

		// Read next token
		token, err = this.readToken(r)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to read token")
		}

		result := &PdfValue{}
		result.Id = obj.Id
		result.Gen = obj.Gen
		result.Type = PDF_TYPE_OBJECT
		result.Value = value

		if token == "stream" {
			result.Type = PDF_TYPE_STREAM

			err = this.skipWhitespace(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to skip whitespace")
			}

			// Get stream length dictionary
			lengthDict := value.Dictionary["/Length"]

			// Get number of bytes of stream
			length := lengthDict.Int

			// If lengthDict is an object reference, resolve the object and set length
			if lengthDict.Type == PDF_TYPE_OBJREF {
				lengthDict, err = this.resolveObject(lengthDict)

				if err != nil {
					return nil, errors.Wrap(err, "Failed to resolve length object of stream")
				}

				// Set length to resolved object value
				length = lengthDict.Value.Int
			}

			// Read length bytes
			bytes := make([]byte, length)

			// Cannot use reader.Read() because that may not read all the bytes
			_, err := io.ReadFull(r, bytes)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read bytes from buffer")
			}

			token, err = this.readToken(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read token")
			}
			if token != "endstream" {
				return nil, errors.New("Expected next token to be: endstream, got: " + token)
			}

			token, err = this.readToken(r)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to read token")
			}

			streamObj := &PdfValue{}
			streamObj.Type = PDF_TYPE_STREAM
			streamObj.Bytes = bytes

			result.Stream = streamObj
		}

		if token != "endobj" {
			return nil, errors.New("Expected next token to be: endobj, got: " + token)
		}

		// Reposition the file pointer to previous position
		_, err = this.f.Seek(old_pos, 0)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to set position of file")
		}

		return result, nil

	} else {
		return objSpec, nil
	}

	return &PdfValue{}, nil
}

// Find the xref offset (should be at the end of the PDF)
func (this *PdfReader) findXref() error {
	var result int
	var err error
	var toRead int64

	toRead = 1500

	// If PDF is smaller than 1500 bytes, be sure to only read the number of bytes that are in the file
	fileSize := this.nBytes
	if fileSize < toRead {
		toRead = fileSize
	}

	// 0 means relative to the origin of the file,
	// 1 means relative to the current offset,
	// and 2 means relative to the end.
	whence := 2

	// Perform seek operation
	_, err = this.f.Seek(-toRead, whence)
	if err != nil {
		return errors.Wrap(err, "Failed to set position of file")
	}

	// Create new bufio.Reader
	r := bufio.NewReader(this.f)
	for {
		// Read all tokens until "startxref" is found
		token, err := this.readToken(r)
		if err != nil {
			return errors.Wrap(err, "Failed to read token")
		}

		if token == "startxref" {
			token, err = this.readToken(r)
			// Probably EOF before finding startxref
			if err != nil {
				return errors.Wrap(err, "Failed to find startxref token")
			}

			// Convert line (string) into int
			result, err = strconv.Atoi(token)
			if err != nil {
				return errors.Wrap(err, "Failed to convert xref position into integer: "+token)
			}

			// Successfully read the xref position
			this.xrefPos = result
			break
		}
	}

	// Rewind file pointer
	whence = 0
	_, err = this.f.Seek(0, whence)
	if err != nil {
		return errors.Wrap(err, "Failed to set position of file")
	}

	this.xrefPos = result

	return nil
}

// Read and parse the xref table
func (this *PdfReader) readXref() error {
	var err error

	// Create new bufio.Reader
	r := bufio.NewReader(this.f)

	// Set file pointer to xref start
	_, err = this.f.Seek(int64(this.xrefPos), 0)
	if err != nil {
		return errors.Wrap(err, "Failed to set position of file")
	}

	// Xref should start with 'xref'
	t, err := this.readToken(r)
	if err != nil {
		return errors.Wrap(err, "Failed to read token")
	}
	if t != "xref" {
		return errors.New("Expected xref to start with 'xref'.  Got: " + t)
	}

	for {
		// Next value will be the starting object id (usually 0, but not always) or the trailer
		t, err = this.readToken(r)
		if err != nil {
			return errors.Wrap(err, "Failed to read token")
		}

		// Check for trailer
		if t == "trailer" {
			break
		}

		// Convert token to int
		startObject, err := strconv.Atoi(t)
		if err != nil {
			return errors.Wrap(err, "Failed to convert start object to integer: "+t)
		}

		// Determine how many objects there are
		t, err = this.readToken(r)
		if err != nil {
			return errors.Wrap(err, "Failed to read token")
		}

		// Convert token to int
		numObject, err := strconv.Atoi(t)
		if err != nil {
			return errors.Wrap(err, "Failed to convert num object to integer: "+t)
		}

		// For all objects in xref, read object position, object generation, and status (free or new)
		for i := startObject; i < startObject+numObject; i++ {
			t, err = this.readToken(r)
			if err != nil {
				return errors.Wrap(err, "Failed to read token")
			}

			// Get object position as int
			objPos, err := strconv.Atoi(t)
			if err != nil {
				return errors.Wrap(err, "Failed to convert object position to integer: "+t)
			}

			t, err = this.readToken(r)
			if err != nil {
				return errors.Wrap(err, "Failed to read token")
			}

			// Get object generation as int
			objGen, err := strconv.Atoi(t)
			if err != nil {
				return errors.Wrap(err, "Failed to convert object generation to integer: "+t)
			}

			// Get object status (free or new)
			objStatus, err := this.readToken(r)
			if err != nil {
				return errors.Wrap(err, "Failed to read token")
			}
			if objStatus != "f" && objStatus != "n" {
				return errors.New("Expected objStatus to be 'n' or 'f', got: " + objStatus)
			}

			// Append map[int]int
			this.xref[i] = make(map[int]int, 1)

			// Set object id, generation, and position
			this.xref[i][objGen] = objPos
		}
	}

	// Read trailer dictionary
	t, err = this.readToken(r)
	if err != nil {
		return errors.Wrap(err, "Failed to read token")
	}

	trailer, err := this.readValue(r, t)
	if err != nil {
		return errors.Wrap(err, "Failed to read value for token: "+t)
	}

	// If /Root is set, then set trailer object so that /Root can be read later
	if _, ok := trailer.Dictionary["/Root"]; ok {
		this.trailer = trailer
	}

	// If a /Prev xref trailer is specified, parse that
	if tr, ok := trailer.Dictionary["/Prev"]; ok {
		// Resolve parent xref table
		this.xrefPos = tr.Int
		return this.readXref()
	}

	return nil
}

// Read root (catalog object)
func (this *PdfReader) readRoot() error {
	var err error

	rootObjSpec := this.trailer.Dictionary["/Root"]

	// Read root (catalog)
	this.catalog, err = this.resolveObject(rootObjSpec)
	if err != nil {
		return errors.Wrap(err, "Failed to resolve root object")
	}

	return nil
}

// Read all pages in PDF
func (this *PdfReader) readPages() error {
	var err error

	// resolve_pages_dict
	pagesDict, err := this.resolveObject(this.catalog.Value.Dictionary["/Pages"])
	if err != nil {
		return errors.Wrap(err, "Failed to resolve pages object")
	}

	// This will normally return itself
	kids, err := this.resolveObject(pagesDict.Value.Dictionary["/Kids"])
	if err != nil {
		return errors.Wrap(err, "Failed to resolve kids object")
	}

	// Allocate pages
	this.pages = make([]*PdfValue, len(kids.Array))

	// Loop through pages and add to result
	for i := 0; i < len(kids.Array); i++ {
		page, err := this.resolveObject(kids.Array[i])
		if err != nil {
			return errors.Wrap(err, "Failed to resolve kid object")
		}

		// Set page
		this.pages[i] = page
	}

	return nil
}

// Get references to page resources for a given page number
func (this *PdfReader) getPageResources(pageno int) (*PdfValue, error) {
	var err error

	// Check to make sure page exists in pages slice
	if len(this.pages) < pageno {
		return nil, errors.New(fmt.Sprintf("Page %d does not exist!!", pageno))
	}

	// Resolve page object
	page, err := this.resolveObject(this.pages[pageno-1])
	if err != nil {
		return nil, errors.Wrap(err, "Failed to resolve page object")
	}

	// Check to see if /Resources exists in Dictionary
	if _, ok := page.Value.Dictionary["/Resources"]; ok {
		// Resolve /Resources object
		res, err := this.resolveObject(page.Value.Dictionary["/Resources"])
		if err != nil {
			return nil, errors.Wrap(err, "Failed to resolve resources object")
		}

		// If type is PDF_TYPE_OBJECT, return its Value
		if res.Type == PDF_TYPE_OBJECT {
			return res.Value, nil
		}

		// Otherwise, returned the resolved object
		return res, nil
	} else {
		// If /Resources does not exist, check to see if /Parent exists and return that
		if _, ok := page.Value.Dictionary["/Parent"]; ok {
			// Resolve parent object
			res, err := this.resolveObject(page.Value.Dictionary["/Parent"])
			if err != nil {
				return nil, errors.Wrap(err, "Failed to resolve parent object")
			}

			// If /Parent object type is PDF_TYPE_OBJECT, return its Value
			if res.Type == PDF_TYPE_OBJECT {
				return res.Value, nil
			}

			// Otherwise, return the resolved parent object
			return res, nil
		}
	}

	// Return an empty PdfValue if we got here
	// TODO:  Improve error handling
	return &PdfValue{}, nil
}

// Get page content and return a slice of PdfValue objects
func (this *PdfReader) getPageContent(objSpec *PdfValue) ([]*PdfValue, error) {
	var err error
	var content *PdfValue

	// Allocate slice
	contents := make([]*PdfValue, 0)

	if objSpec.Type == PDF_TYPE_OBJREF {
		// If objSpec is an object reference, resolve the object and append it to contents
		content, err = this.resolveObject(objSpec)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to resolve object")
		}
		contents = append(contents, content)
	} else if objSpec.Type == PDF_TYPE_ARRAY {
		// If objSpec is an array, loop through the array and recursively get page content and append to contents
		for i := 0; i < len(objSpec.Array); i++ {
			tmpContents, err := this.getPageContent(objSpec.Array[i])
			if err != nil {
				return nil, errors.Wrap(err, "Failed to get page content")
			}
			for j := 0; j < len(tmpContents); j++ {
				contents = append(contents, tmpContents[j])
			}
		}
	}

	return contents, nil
}

// Get content (i.e. PDF drawing instructions)
func (this *PdfReader) getContent(pageno int) (string, error) {
	var err error
	var contents []*PdfValue

	// Check to make sure page exists in pages slice
	if len(this.pages) < pageno {
		return "", errors.New(fmt.Sprintf("Page %d does not exist.", pageno))
	}

	// Get page
	page := this.pages[pageno-1]

	// FIXME: This could be slow, converting []byte to string and appending many times
	buffer := ""

	// Check to make sure /Contents exists in page dictionary
	if _, ok := page.Value.Dictionary["/Contents"]; ok {
		// Get an array of page content
		contents, err = this.getPageContent(page.Value.Dictionary["/Contents"])
		if err != nil {
			return "", errors.Wrap(err, "Failed to get page content")
		}

		for i := 0; i < len(contents); i++ {
			// Decode content if one or more /Filter is specified.
			// Most common filter is FlateDecode which can be uncompressed with zlib
			tmpBuffer, err := this.rebuildContentStream(contents[i])
			if err != nil {
				return "", errors.Wrap(err, "Failed to rebuild content stream")
			}

			// FIXME:  This is probably slow
			buffer += string(tmpBuffer)
		}
	}

	return buffer, nil
}

// Rebuild content stream
// This will decode content if one or more /Filter (such as FlateDecode) is specified.
// If there are multiple filters, they will be decoded in the order in which they were specified.
func (this *PdfReader) rebuildContentStream(content *PdfValue) ([]byte, error) {
	var err error
	var tmpFilter *PdfValue

	// Allocate slice of PdfValue
	filters := make([]*PdfValue, 0)

	// If content has a /Filter, append it to filters slice
	if _, ok := content.Value.Dictionary["/Filter"]; ok {
		filter := content.Value.Dictionary["/Filter"]

		// If filter type is a reference, resolve it
		if filter.Type == PDF_TYPE_OBJREF {
			tmpFilter, err = this.resolveObject(filter)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to resolve object")
			}
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
			return nil, errors.New("Unspported filter: " + filters[i].Token)
		}
	}

	return stream, nil
}

func (this *PdfReader) getAllPageBoxes(k float64) (map[int]map[string]map[string]float64, error) {
	var err error

	// Allocate result with the number of available boxes
	result := make(map[int]map[string]map[string]float64, len(this.pages))

	for i := 1; i <= len(this.pages); i++ {
		result[i], err = this.getPageBoxes(i, k)
		if result[i] == nil {
			return nil, errors.Wrap(err, "Unable to get page box")
		}
	}

	return result, nil
}

// Get all page box data
func (this *PdfReader) getPageBoxes(pageno int, k float64) (map[string]map[string]float64, error) {
	var err error

	// Allocate result with the number of available boxes
	result := make(map[string]map[string]float64, len(this.availableBoxes))

	// Check to make sure page exists in pages slice
	if len(this.pages) < pageno {
		return nil, errors.New(fmt.Sprintf("Page %d does not exist?", pageno))
	}

	// Resolve page object
	page, err := this.resolveObject(this.pages[pageno-1])
	if err != nil {
		return nil, errors.New("Failed to resolve page object")
	}

	// Loop through available boxes and add to result
	for i := 0; i < len(this.availableBoxes); i++ {
		box, err := this.getPageBox(page, this.availableBoxes[i], k)
		if err != nil {
			return nil, errors.New("Failed to get page box")
		}

		result[this.availableBoxes[i]] = box
	}

	return result, nil
}

// Get a specific page box value (e.g. MediaBox) and return its values
func (this *PdfReader) getPageBox(page *PdfValue, box_index string, k float64) (map[string]float64, error) {
	var err error
	var tmpBox *PdfValue

	// Allocate 8 fields in result
	result := make(map[string]float64, 8)

	// Check to make sure box_index (e.g. MediaBox) exists in page dictionary
	if _, ok := page.Value.Dictionary[box_index]; ok {
		box := page.Value.Dictionary[box_index]

		// If the box type is a reference, resolve it
		if box.Type == PDF_TYPE_OBJREF {
			tmpBox, err = this.resolveObject(box)
			if err != nil {
				return nil, errors.New("Failed to resolve object")
			}
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
		} else {
			// TODO: Improve error handling
			return nil, errors.New("Could not get page box")
		}
	} else if _, ok := page.Value.Dictionary["/Parent"]; ok {
		parentObj, err := this.resolveObject(page.Value.Dictionary["/Parent"])
		if err != nil {
			return nil, errors.Wrap(err, "Could not resolve parent object")
		}

		// If the page box is inherited from /Parent, recursively return page box of parent
		return this.getPageBox(parentObj, box_index, k)
	}

	return result, nil
}

// Get page rotation for a page number
func (this *PdfReader) getPageRotation(pageno int) (*PdfValue, error) {
	// Check to make sure page exists in pages slice
	if len(this.pages) < pageno {
		return nil, errors.New(fmt.Sprintf("Page %d does not exist!!!!", pageno))
	}

	return this._getPageRotation(this.pages[pageno-1])
}

// Get page rotation for a page object spec
func (this *PdfReader) _getPageRotation(page *PdfValue) (*PdfValue, error) {
	var err error

	// Resolve page object
	page, err = this.resolveObject(page)
	if err != nil {
		return nil, errors.New("Failed to resolve page object")
	}

	// Check to make sure /Rotate exists in page dictionary
	if _, ok := page.Value.Dictionary["/Rotate"]; ok {
		res, err := this.resolveObject(page.Value.Dictionary["/Rotate"])
		if err != nil {
			return nil, errors.New("Failed to resolve rotate object")
		}

		// If the type is PDF_TYPE_OBJECT, return its value
		if res.Type == PDF_TYPE_OBJECT {
			return res.Value, nil
		}

		// Otherwise, return the object
		return res, nil
	} else {
		// Check to see if parent has a rotation
		if _, ok := page.Value.Dictionary["/Parent"]; ok {
			// Recursively return /Parent page rotation
			res, err := this._getPageRotation(page.Value.Dictionary["/Parent"])
			if err != nil {
				return nil, errors.Wrap(err, "Failed to get page rotation for parent")
			}

			// If the type is PDF_TYPE_OBJECT, return its value
			if res.Type == PDF_TYPE_OBJECT {
				return res.Value, nil
			}

			// Otherwise, return the object
			return res, nil
		}
	}

	return &PdfValue{Int: 0}, nil
}

func (this *PdfReader) read() error {
	var err error
	// Find xref position
	err = this.findXref()
	if err != nil {
		return errors.Wrap(err, "Failed to find xref position")
	}
	// Parse xref table
	err = this.readXref()
	if err != nil {
		return errors.Wrap(err, "Failed to read xref table")
	}

	// Read catalog
	err = this.readRoot()
	if err != nil {
		return errors.Wrap(err, "Failed to read root")
	}

	// Read pages
	err = this.readPages()
	if err != nil {
		return errors.Wrap(err, "Failed to to read pages")
	}

	return nil
}
