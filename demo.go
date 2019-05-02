package gofpdi

import (
	"fmt"
	"github.com/pkg/errors"
)

func Demo() (*PdfWriter, error) {
	var err error

	// Create new reader
	reader, err := NewPdfReader("/Users/dave/Desktop/PDFPL110.pdf")
	if err != nil {
		return nil, errors.Wrap(err, "Unable to create new pdf reader")
	}

	// Create new writer
	writer, err := NewPdfWriter("/Users/dave/Desktop/pdfwriter-output.pdf")
	if err != nil {
		return nil, errors.Wrap(err, "Unable to create new pdf writer")
	}

	tpl, err := writer.ImportPage(reader, 1, "/CropBox")
	if err != nil {
		return nil, errors.Wrap(err, "Unable to import page")
	}

	writer.out("%PDF-1.4\n%ABCD\n\n")

	_, err = writer.PutFormXobjects(reader)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to put form xobjects")
	}

	pagesObjId := writer.n + 1
	pageObjId := writer.n + 2
	contentsObjId := writer.n + 3
	catalogObjId := writer.n + 4

	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", pageObjId))
	writer.out("endobj")

	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /LastModified (D:20190412184239+00'00') /Resources %s /MediaBox [0.000000 0.000000 1319.976000 887.976000] /CropBox [0.000000 0.000000 1319.976000 887.976000] /BleedBox [20.988000 20.988000 1298.988000 866.988000] /TrimBox [29.988000 29.988000 1289.988000 857.988000] /Contents %d 0 R /Rotate 0 /Group << /Type /Group /S /Transparency /CS /DeviceRGB >> /PZ 1 >>", pagesObjId, fmt.Sprintf("<</ProcSet [/PDF /Text /ImageB /ImageC /ImageI ] /XObject <</TPL1 %d 0 R>>>>", 1), contentsObjId))
	writer.out("endobj")

	str := writer.useTemplate(tpl, 1, 0.0, -887.976*0.5, 1319.976*0.5, 0.0)

	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<</Length %d>>\nstream", len(str)))
	writer.out(str)
	writer.out("endstream")
	writer.out("endobj")

	// draw template

	// put catalog
	writer.newObj(-1, false)
	writer.out(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObjId))

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

	writer.w.Flush()

	return writer, nil
}
