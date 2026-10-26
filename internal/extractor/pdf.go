package extractor

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"

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
func (r *PDFRenderer) RenderFirstPagePNG(ctx context.Context, pdfData []byte) ([]byte, error) {
	instance, err := r.pool.GetInstanceWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pdfium instance: %w", err)
	}
	defer instance.Close()

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &pdfData})
	if err != nil {
		return nil, fmt.Errorf("open pdf document: %w", err)
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	count, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, err
	}
	if count.PageCount != 1 {
		return nil, fmt.Errorf("only single-page invoice PDFs are supported; split this document first")
	}
	size, err := instance.FPDF_GetPageSizeByIndex(&requests.FPDF_GetPageSizeByIndex{Document: doc.Document, Index: 0})
	if err != nil {
		return nil, err
	}
	width, height := size.Width*150/72, size.Height*150/72
	if math.IsNaN(width) || math.IsNaN(height) || width <= 0 || height <= 0 || width > 10000 || height > 10000 || width*height > 25_000_000 {
		return nil, fmt.Errorf("PDF page dimensions exceed rendering limits")
	}
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return *render.ImageBytes, nil
}

// openFile is a tiny indirection kept for testability.
var openFile = os.Open
