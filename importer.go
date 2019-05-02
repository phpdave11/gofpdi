package gofpdi

type Importer struct {
	sourceFile string
	readers map[string]*PdfReader
	writer *PdfWriter
}

func (this *Importer) GetReader() *PdfReader {
	return this.GetReaderForFile(this.sourceFile)
}

func (this *Importer) GetWriter() *PdfWriter {
	return this.writer
}

func (this *Importer) GetReaderForFile(file string) *PdfReader {
    if _, ok := this.readers[file]; ok {
		return this.readers[file]
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
	this.writer, _ = NewPdfWriter("")
}

func (this *Importer) SetSourceFile(f string) {
	this.sourceFile = f

	// If reader hasn't been instantiated, do that now
    if _, ok := this.readers[this.sourceFile]; !ok {
		reader, _ := NewPdfReader(this.sourceFile)
		this.readers[this.sourceFile] = reader
	}
}

func (this *Importer) ImportPage(pageno int, box string) string {
	this.writer.ImportPage(this.GetReader(), pageno, box)

	// FIXME:  Make this not hard coded
	return "/GOFPDITPL0"
}

func (this *Importer) SetNextObjectID(objId int) {
	this.GetWriter().SetNextObjectID(objId)
}

func (this *Importer) PutFormXobjects() map[string]int {
	res, _ := this.GetWriter().PutFormXobjects(this.GetReader())
	return res
}

func (this *Importer) GetImportedObjects() map[int]string {
	return this.GetWriter().GetImportedObjects()
}

func (this *Importer) UseTemplate(tplName string, _x float64, _y float64, _w float64, _h float64) (string, float64, float64, float64, float64) {
	return this.GetWriter().UseTemplate(tplName, _x, _y, _w, _h)
}
