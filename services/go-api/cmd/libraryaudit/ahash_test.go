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

func flat(size int, level uint8) image.Image {
	img := image.NewGray(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetGray(x, y, color.Gray{Y: level})
		}
	}
	return img
}

func TestSignatureOf_FlatImagesAreBlankAndRealArtworkIsNot(t *testing.T) {
	cases := []struct {
		name  string
		img   image.Image
		blank bool
	}{
		{"white", flat(64, 255), true},
		{"black", flat(64, 0), true},
		{"grey placeholder", flat(64, 128), true},
		{"gradient artwork", gradient(64, false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, spread := signatureOf(tc.img)
			if isBlank(spread) != tc.blank {
				t.Fatalf("spread %f", spread)
			}
		})
	}
}

func TestSignatureOf_KeepsTheSameHashAsAverageHash(t *testing.T) {
	hash, _ := signatureOf(gradient(120, false))
	if hash != averageHash(gradient(120, false)) {
		t.Fatalf("hash %x", hash)
	}
}
