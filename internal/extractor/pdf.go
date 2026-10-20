package extractor

import (
	"fmt"
	"os"
	"sync"
	"time"

	gopdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// PDFRenderer renders PDF pages to PNG images using the WebAssembly build
// of PDFium, keeping the application binary CGO-free.
type PDFRenderer struct {
	pool gopdfium.Pool
}

var (
	pdfOnce     sync.Once
	pdfRenderer *PDFRenderer
	pdfInitErr  error
)

// SharedRenderer lazily initializes and returns the shared PDF renderer.
// Initialization happens on first use so that the binary starts fast and
// only pays the WASM setup cost when a PDF is actually imported.
func SharedRenderer() (*PDFRenderer, error) {
	pdfOnce.Do(func() {
		pool, err := webassembly.Init(webassembly.Config{
			MinIdle:  1,
			MaxIdle:  1,
			MaxTotal: 2,
		})
		if err != nil {
			pdfInitErr = fmt.Errorf("init pdfium webassembly: %w", err)
			return
		}
		pdfRenderer = &PDFRenderer{pool: pool}
	})
	return pdfRenderer, pdfInitErr
}

// RenderFirstPagePNG renders page 0 of the given PDF at 150 DPI and
// returns PNG-encoded bytes suitable for a multimodal LLM call.
func (r *PDFRenderer) RenderFirstPagePNG(pdfData []byte) ([]byte, error) {
	instance, err := r.pool.GetInstance(30 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("get pdfium instance: %w", err)
	}
	defer instance.Close()

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &pdfData})
	if err != nil {
		return nil, fmt.Errorf("open pdf document: %w", err)
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	render, err := instance.RenderToFile(&requests.RenderToFile{
		RenderPageInDPI: &requests.RenderPageInDPI{
			Page: requests.Page{
				ByIndex: &requests.PageByIndex{Document: doc.Document, Index: 0},
			},
			DPI: 150,
		},
		OutputFormat: requests.RenderToFileOutputFormatPNG,
		OutputTarget: requests.RenderToFileOutputTargetBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("render pdf page: %w", err)
	}
	if render.ImageBytes == nil || len(*render.ImageBytes) == 0 {
		return nil, fmt.Errorf("render pdf page: empty output")
	}
	return *render.ImageBytes, nil
}

// openFile is a tiny indirection kept for testability.
var openFile = os.Open
