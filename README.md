# gofpdi

## Go Free PDF Document Importer

Based on [fpdi](https://github.com/Setasign/FPDI/tree/1.6.x-legacy)

### Example

```
package main

import (
	"compress/zlib"
	"fmt"
	"github.com/phpdave11/gofpdf"
)

func SetDefaultFont(pdf *gofpdf.Fpdf) error {
	err := pdf.AddTTFFont("Arial", "/Library/Fonts/Arial.ttf")
	if err != nil {
		return err
	}

	err = pdf.SetFont("Arial", "", 30)
	if err != nil {
		return err
	}

	return nil
}

func GeneratePDF() error {
	var err error

	// Create new PDF instance
	width := 8.5
	height := 11.0
	pdf, err := gofpdf.New(
		gofpdf.PdfOptionUnit(gofpdf.Unit_IN),
		gofpdf.PdfOptionPageSize(width, height),
		gofpdf.PdfOptionCompress(zlib.DefaultCompression),
		gofpdf.PdfOptionCreator("DavePDF"),
	)

	// Import page from PDF
	tpl1 := pdf.ImportPage("/Users/dave/Desktop/PDFPL108.pdf", 1, "/TrimBox")

	// Import page from PDF
	tpl2 := pdf.ImportPage("/Users/dave/Desktop/PDFPL110.pdf", 1, "/TrimBox")

	// Import page from PDF
	tpl3 := pdf.ImportPage("/Users/dave/Desktop/PDFPL115.pdf", 1, "/TrimBox")

	// Import page from PDF - same PDF file, different pages
	tpl4 := pdf.ImportPage("/Users/dave/Desktop/PDF280.pdf", 1, "/TrimBox")
	tpl5 := pdf.ImportPage("/Users/dave/Desktop/PDF280.pdf", 2, "/TrimBox")

	// Add Page
	pdf.AddPage()

	// Use Imported templates
	pdf.UseImportedTemplate(tpl1, 0.5, 5.5, 0, 1.5)
	pdf.UseImportedTemplate(tpl2, 3.5, 5.5, 0, 1.5)
	pdf.UseImportedTemplate(tpl3, 0.5, 8.5, 0, 1.5)
	pdf.UseImportedTemplate(tpl4, 3.5, 8.5, 0, 1.5)
	pdf.UseImportedTemplate(tpl5, 5.0, 8.5, 0, 1.5)

	// Set font
	err = SetDefaultFont(pdf)
	if err != nil {
		return err
	}

	// Write some text
	pdf.SetX(1)
	pdf.SetY(1)
	pdf.Cell(1, 1, "Created with phpdave11/gofpdf")

	// Draw a line
	pdf.Line(0, 0, 5, 5)

	pdf.WritePdf("/Users/dave/Desktop/output.pdf")

	return nil
}

func main() {
	var err error

	err = GeneratePDF()
	if err != nil {
		fmt.Println(err)
	}
}

```
