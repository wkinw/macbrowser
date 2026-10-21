package thumbs

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"io"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const maxPixels = 150_000_000

var (
	ErrTooLarge  = errors.New("影像過大")
	ErrNoPreview = errors.New("沒有可預覽的圖片")
)

func Render(r io.Reader, size int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, 512<<20))
	if err != nil {
		return nil, err
	}
	img, err := decodeImage(data)
	if err != nil {
		return nil, err
	}
	img = shrink(img, size*2)
	img = imaging.Fit(img, size, size, imaging.Lanczos)
	bounds := img.Bounds()
	flat := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, bounds.Min, draw.Over)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, flat, imaging.JPEG, imaging.JPEGQuality(86)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type folderCell struct {
	x, y, w, h int
}

func RenderFolder(images [][]byte, size int) ([]byte, error) {
	if size <= 0 {
		size = 256
	}
	gap := size / 64
	if gap < 2 {
		gap = 2
	}
	half := (size - 3*gap) / 2
	var cells []folderCell
	usable := size - 2*gap
	switch len(images) {
	case 0:
		return nil, ErrNoPreview
	case 1:
		cells = []folderCell{{gap, gap, usable, usable}}
	case 2:
		cells = []folderCell{
			{gap, gap, half, usable},
			{half + 2*gap, gap, half, usable},
		}
	case 3:
		cells = []folderCell{
			{gap, gap, half, half},
			{half + 2*gap, gap, half, half},
			{gap, half + 2*gap, half, half},
		}
	default:
		cells = []folderCell{
			{gap, gap, half, half},
			{half + 2*gap, gap, half, half},
			{gap, half + 2*gap, half, half},
			{half + 2*gap, half + 2*gap, half, half},
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
	ok := 0
	for i, data := range images {
		if i >= len(cells) {
			break
		}
		img, err := decodeImage(data)
		if err != nil {
			continue
		}
		cell := cells[i]
		maxSide := cell.w
		if cell.h > maxSide {
			maxSide = cell.h
		}
		img = shrink(img, maxSide*2)
		img = imaging.Fit(img, cell.w, cell.h, imaging.Lanczos)
		b := img.Bounds()
		x := cell.x + (cell.w-b.Dx())/2
		y := cell.y + (cell.h-b.Dy())/2
		draw.Draw(canvas, image.Rect(x, y, x+b.Dx(), y+b.Dy()), img, b.Min, draw.Src)
		ok++
	}
	if ok == 0 {
		return nil, ErrNoPreview
	}
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, canvas, imaging.JPEG, imaging.JPEGQuality(86)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeImage(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, ErrTooLarge
	}
	return imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
}

func shrink(img image.Image, maxSide int) image.Image {
	for {
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		if w <= maxSide && h <= maxSide {
			return img
		}
		nw, nh := w/2, h/2
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		img = imaging.Resize(img, nw, nh, imaging.Box)
	}
}
