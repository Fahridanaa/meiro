package artwork

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func solid(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{200, 100, 50, 255}), image.Point{}, draw.Src)
	return img
}

func TestFitCropsShrinksAndRounds(t *testing.T) {
	got := fit(solid(400, 200), 100, 0.25)
	if got.Bounds().Dx() != 100 || got.Bounds().Dy() != 100 {
		t.Fatalf("size = %v, want 100x100", got.Bounds())
	}
	if a := got.RGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want 0", a)
	}
	if a := got.RGBAAt(50, 50).A; a != 255 {
		t.Errorf("centre alpha = %d, want 255", a)
	}
	if a := got.RGBAAt(50, 0).A; a != 255 {
		t.Errorf("edge middle alpha = %d, want 255", a)
	}
}

func TestFitKeepsSmallPictureAndMakesDisc(t *testing.T) {
	got := fit(solid(60, 60), 320, 0.5)
	if got.Bounds().Dx() != 60 {
		t.Fatalf("size = %v, want 60 (not enlarged)", got.Bounds())
	}
	if got.RGBAAt(0, 59).A != 0 || got.RGBAAt(30, 30).A != 255 {
		t.Error("a corner of 0.5 should make a disc")
	}
}
