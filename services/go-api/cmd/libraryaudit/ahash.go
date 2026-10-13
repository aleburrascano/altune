package main

import (
	"image"
	"math/bits"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

const (
	hashGrid          = 8
	sameImageDistance = 6
)

func averageHash(img image.Image) uint64 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return 0
	}
	return bitsAboveMean(gridLuminance(img, bounds, width, height))
}

func gridLuminance(img image.Image, bounds image.Rectangle, width, height int) []float64 {
	cells := make([]float64, 0, hashGrid*hashGrid)
	for row := range hashGrid {
		for col := range hashGrid {
			cells = append(cells, cellLuminance(img, bounds, col, row, width, height))
		}
	}
	return cells
}

func bitsAboveMean(cells []float64) uint64 {
	mean := 0.0
	for _, cell := range cells {
		mean += cell
	}
	mean /= float64(len(cells))
	var hash uint64
	for i, cell := range cells {
		if cell > mean {
			hash |= 1 << uint(i)
		}
	}
	return hash
}

func cellLuminance(img image.Image, bounds image.Rectangle, col, row, width, height int) float64 {
	x0 := bounds.Min.X + col*width/hashGrid
	x1 := max(bounds.Min.X+(col+1)*width/hashGrid, x0+1)
	y0 := bounds.Min.Y + row*height/hashGrid
	y1 := max(bounds.Min.Y+(row+1)*height/hashGrid, y0+1)
	sum := 0.0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			sum += 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		}
	}
	return sum / float64((x1-x0)*(y1-y0))
}

func hashDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

func isSameImage(a, b uint64) bool {
	return hashDistance(a, b) <= sameImageDistance
}
