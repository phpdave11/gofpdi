# gofpdi

## Go Free PDF Document Importer

Based on [fpdi](https://github.com/Setasign/FPDI/tree/1.6.x-legacy)

gofpdi allows you to import an existing PDF into a new PDF.  Currently the following PDF generation libraries are supported:

[gopdf](https://github.com/signintech/gopdf)

[gofpdf](https://github.com/jung-kurt/gofpdf)



### gopdf example

```
package main

import (
        "github.com/signintech/gopdf"
        "io"
        "net/http"
        "os"
)

func main() {
        var err error

        // Download a Font
        fontUrl := "https://github.com/google/fonts/raw/master/ofl/daysone/DaysOne-Regular.ttf"
        if err = DownloadFile("example-font.ttf", fontUrl); err != nil {
                panic(err)
        }

        // Download a PDF
        fileUrl := "https://tcpdf.org/files/examples/example_012.pdf"
        if err = DownloadFile("example-pdf.pdf", fileUrl); err != nil {
                panic(err)
        }

        pdf := gopdf.GoPdf{}
        pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 595.28, H: 841.89}}) //595.28, 841.89 = A4

        pdf.AddPage()

        err = pdf.AddTTFFont("daysone", "example-font.ttf")
        if err != nil {
                panic(err)
        }

        err = pdf.SetFont("daysone", "", 20)
        if err != nil {
                panic(err)
        }

        // Color the page
        pdf.SetLineWidth(0.1)
        pdf.SetFillColor(124, 252, 0) //setup fill color
        pdf.RectFromUpperLeftWithStyle(50, 100, 400, 600, "FD")
        pdf.SetFillColor(0, 0, 0)

        pdf.SetX(50)
        pdf.SetY(50)
        pdf.Cell(nil, "Import existing PDF into GoPDF Document")

        // Import page 1
        tpl1 := pdf.ImportPage("example-pdf.pdf", 1, "/MediaBox")

        // Draw pdf onto page
        pdf.UseImportedTemplate(tpl1, 50, 100, 400, 0)

        pdf.WritePdf("example.pdf")

}

// DownloadFile will download a url to a local file. It's efficient because it will
// write as it downloads and not load the whole file into memory.
func DownloadFile(filepath string, url string) error {
        // Get the data
        resp, err := http.Get(url)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        // Create the file
        out, err := os.Create(filepath)
        if err != nil {
                return err
        }
        defer out.Close()

        // Write the body to file
        _, err = io.Copy(out, resp.Body)
        return err
}
```

Generated PDF: [example.pdf](https://github.com/signintech/gopdf/files/3144466/example.pdf)

Screenshot of PDF:

![example](https://user-images.githubusercontent.com/9421180/57180557-4c1dbd80-6e4f-11e9-8f47-9d40217805be.jpg)

### gofpdf example

```
package main

import (
	"github.com/jung-kurt/gofpdf"
	"github.com/jung-kurt/gofpdf/contrib/gofpdi"
	"io"
	"net/http"
	"os"
)

func main() {
	var err error

	pdf := gofpdf.New("P", "mm", "A4", "")

	// Download a PDF
	fileUrl := "https://tcpdf.org/files/examples/example_026.pdf"
	if err = DownloadFile("example-pdf.pdf", fileUrl); err != nil {
		panic(err)
	}

	// Import example-pdf.pdf with gofpdi free pdf document importer
	tpl1 := gofpdi.ImportPage(pdf, "example-pdf.pdf", 1, "/MediaBox")

	pdf.AddPage()

	pdf.SetFillColor(200, 700, 220)
	pdf.Rect(20, 50, 150, 215, "F")

	// Draw imported template onto page
	gofpdi.UseImportedTemplate(pdf, tpl1, 20, 50, 150, 0)

	pdf.SetFont("Helvetica", "", 20)
	pdf.Cell(0, 0, "Import existing PDF into gofpdf document with gofpdi")

	err = pdf.OutputFileAndClose("example.pdf")
	if err != nil {
		panic(err)
	}
}

// DownloadFile will download a url to a local file. It's efficient because it will
// write as it downloads and not load the whole file into memory.
func DownloadFile(filepath string, url string) error {
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}
```

Generated PDF:  [example.pdf](https://github.com/jung-kurt/gofpdf/files/3178770/example.pdf)

Screenshot of PDF:
![example](https://user-images.githubusercontent.com/9421180/57713804-ca8d1300-7638-11e9-9f8e-e3f803374803.jpg)
