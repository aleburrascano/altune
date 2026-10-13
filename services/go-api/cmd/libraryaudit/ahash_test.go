package main

import (
	"image"
	"image/color"
	"testing"
)

func gradient(size int, invert bool) image.Image {
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			v := uint8(x * 255 / (size - 1))
			if invert {
				v = 255 - v
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return img
}

func TestAverageHash_SameImageAtAnotherSizeIsTheSameImage(t *testing.T) {
	small, large := averageHash(gradient(64, false)), averageHash(gradient(300, false))
	if !isSameImage(small, large) {
		t.Fatalf("distance %d", hashDistance(small, large))
	}
}

func TestAverageHash_DifferentImageIsNotTheSameImage(t *testing.T) {
	a, b := averageHash(gradient(64, false)), averageHash(gradient(64, true))
	if isSameImage(a, b) {
		t.Fatalf("distance %d", hashDistance(a, b))
	}
}

func TestAverageHash_EmptyImageHashesToZero(t *testing.T) {
	if got := averageHash(image.NewGray(image.Rect(0, 0, 0, 0))); got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestHashDistance_CountsDifferingBits(t *testing.T) {
	if got := hashDistance(0b1010, 0b0110); got != 2 {
		t.Fatalf("got %d", got)
	}
}
