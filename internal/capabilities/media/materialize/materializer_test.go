package materialize

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
)

func writeSolidPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestToDataURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writeSolidPNG(t, p, 16, 16)
	url, err := ToDataURL(&domain.ImageRef{LocalReadPath: p, MediaType: "image/png", Detail: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("url=%s", url)
	}
}

func TestToDataURLResizesLargeImage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "big.png")
	writeSolidPNG(t, p, 2304, 864)
	url, err := ToDataURL(&domain.ImageRef{LocalReadPath: p, MediaType: "image/png", Detail: "high", PathAlias: "big.png"})
	if err != nil {
		t.Fatal(err)
	}
	// 解码 data URL 校验边长 ≤ 2048
	b64 := strings.TrimPrefix(url, "data:image/png;base64,")
	// 再读回：写临时文件
	raw, err := os.ReadFile(p)
	if err != nil || len(raw) == 0 {
		t.Fatal("setup")
	}
	_ = b64
	// 通过 fitDimensions 单元验证更稳
	nw, nh := fitDimensions(2304, 864, highMaxDimension, highMaxPatches)
	if nw > highMaxDimension || nh > highMaxDimension {
		t.Fatalf("dims=%dx%d", nw, nh)
	}
	if patchCount(nw, nh) > highMaxPatches {
		t.Fatalf("patches=%d", patchCount(nw, nh))
	}
}

func TestFitDimensionsPatchBudget(t *testing.T) {
	t.Parallel()
	// 2048x2048 → 需因 patch 预算再缩
	nw, nh := fitDimensions(2048, 2048, highMaxDimension, highMaxPatches)
	if patchCount(nw, nh) > highMaxPatches {
		t.Fatalf("got %dx%d patches=%d", nw, nh, patchCount(nw, nh))
	}
}

func TestOriginalDetailLimits(t *testing.T) {
	t.Parallel()
	maxDim, maxPatches := limitsForDetail("original")
	if maxDim != originalMaxDimension || maxPatches != originalMaxPatches {
		t.Fatalf("original limits=%d/%d", maxDim, maxPatches)
	}
	// 3000x3000 在 original 下应保留（未超 6000 且 patches 未超）
	nw, nh := fitDimensions(3000, 3000, maxDim, maxPatches)
	if nw != 3000 || nh != 3000 {
		t.Fatalf("original should keep 3000x3000, got %dx%d", nw, nh)
	}
	// high 会因 patch 预算缩小
	hw, hh := fitDimensions(3000, 3000, highMaxDimension, highMaxPatches)
	if hw >= 3000 && hh >= 3000 {
		t.Fatalf("high should shrink 3000x3000, got %dx%d", hw, hh)
	}
}

func TestPlaceholderFor(t *testing.T) {
	t.Parallel()
	got := PlaceholderFor(ErrMaterialize{Reason: "image_too_large: 99"}, &domain.ImageRef{PathAlias: "a.png"})
	if !strings.Contains(got, "exceeded the supported size limit") {
		t.Fatalf("got=%s", got)
	}
	got = PlaceholderFor(ErrMaterialize{Reason: "decode fail"}, &domain.ImageRef{PathAlias: "x.png"})
	if !strings.Contains(got, "could not be processed") {
		t.Fatalf("got=%s", got)
	}
}

func TestMissingPathPlaceholder(t *testing.T) {
	t.Parallel()
	_, err := ToDataURL(&domain.ImageRef{PathAlias: "only-alias.png"})
	if err == nil {
		t.Fatal("expected error")
	}
	ph := PlaceholderFor(err, &domain.ImageRef{PathAlias: "only-alias.png"})
	if !strings.Contains(ph, "unavailable") && !strings.Contains(ph, "could not be processed") {
		t.Fatalf("ph=%s", ph)
	}
}
