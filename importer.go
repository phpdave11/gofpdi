package gofpdi

import (
	"fmt"
)

type Importer struct {
	sourceFile string
	readers    map[string]*PdfReader
	writers    map[string]*PdfWriter
	tplMap     map[int]*TplInfo
	tplN       int
	writer     *PdfWriter
}

type TplInfo struct {
	SourceFile string
	Writer     *PdfWriter
	TemplateId int
}

func (this *Importer) GetReader() *PdfReader {
	return this.GetReaderForFile(this.sourceFile)
}

func (this *Importer) GetWriter() *PdfWriter {
	return this.GetWriterForFile(this.sourceFile)
}

func (this *Importer) GetReaderForFile(file string) *PdfReader {
	if _, ok := this.readers[file]; ok {
		return this.readers[file]
	}

	return nil
}

func (this *Importer) GetWriterForFile(file string) *PdfWriter {
	if _, ok := this.writers[file]; ok {
		return this.writers[file]
	}

	return nil
}

func NewImporter() *Importer {
	importer := &Importer{}
	importer.init()

	return importer
}

func (this *Importer) init() {
	this.readers = make(map[string]*PdfReader, 0)
	this.writers = make(map[string]*PdfWriter, 0)
	this.tplMap = make(map[int]*TplInfo, 0)
	this.writer, _ = NewPdfWriter("")
}

func (this *Importer) SetSourceFile(f string) {
	this.sourceFile = f

	// If reader hasn't been instantiated, do that now
	if _, ok := this.readers[this.sourceFile]; !ok {
		reader, err := NewPdfReader(this.sourceFile)
		if err != nil {
			panic(err)
		}
		this.readers[this.sourceFile] = reader
	}

	// If writer hasn't been instantiated, do that now
	if _, ok := this.writers[this.sourceFile]; !ok {
		writer, err := NewPdfWriter("")

		// Make the next writer start template numbers at this.tplN
		writer.SetTplIdOffset(this.tplN)
		if err != nil {
			panic(err)
		}
		this.writers[this.sourceFile] = writer
	}
}

func (this *Importer) ImportPage(pageno int, box string) int {
	res, err := this.GetWriter().ImportPage(this.GetReader(), pageno, box)
	if err != nil {
		panic(err)
	}

	// Get current template id
	tplN := this.tplN

	// Set tpl info
	this.tplMap[tplN] = &TplInfo{SourceFile: this.sourceFile, TemplateId: res, Writer: this.GetWriter()}

	// Increment template id
	this.tplN++

	return tplN
}

func (this *Importer) SetNextObjectID(objId int) {
	this.GetWriter().SetNextObjectID(objId)
}

func (this *Importer) PutFormXobjects() map[string]int {
	res, _ := this.GetWriter().PutFormXobjects(this.GetReader())
	return res
}

func (this *Importer) GetImportedObjects() map[int]string {
	res := this.GetWriter().GetImportedObjects()
	this.GetWriter().ClearImportedObjects()
	return res
}

func (this *Importer) UseTemplate(tplid int, _x float64, _y float64, _w float64, _h float64) (string, float64, float64, float64, float64) {
	// First, look up template id in importer tpl map
	tplInfo := this.tplMap[tplid]

	return tplInfo.Writer.UseTemplate(tplInfo.TemplateId, _x, _y, _w, _h)
}
