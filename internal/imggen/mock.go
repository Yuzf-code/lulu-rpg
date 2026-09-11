package imggen

import (
	"bytes"
	"context"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Mock 在本地绘制一张程序化的“概念图”占位：根据提示词哈希变化配色与构图，
// 让自动配图流程无需外部服务即可演示。
type Mock struct{}

// NewMock 创建演示画师。
func NewMock() *Mock { return &Mock{} }

func (m *Mock) Generate(_ context.Context, prompt string) ([]byte, error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(prompt))
	seed := h.Sum32()

	w, he := 768, 768
	img := image.NewRGBA(image.Rect(0, 0, w, he))

	// 夜空渐变
	hueA := color.RGBA{R: uint8(18 + seed%40), G: uint8(16 + seed%30), B: uint8(48 + seed%60), A: 255}
	hueB := color.RGBA{R: uint8(90 + seed%80), G: uint8(50 + seed%40), B: uint8(120 + seed%80), A: 255}
	for y := 0; y < he; y++ {
		t := float32(y) / float32(he)
		c := lerp(hueA, hueB, t*t)
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	// 星星
	rng := uint32(1)
	next := func() uint32 { rng ^= rng << 13; rng ^= rng >> 17; rng ^= rng << 5; return rng }
	for i := 0; i < 140; i++ {
		x := int(next() % uint32(w))
		y := int(next() % uint32(he/3))
		b := uint8(120 + next()%135)
		img.Set(x, y, color.RGBA{b, b, 255 - b/3, 255})
	}
	// 月亮
	moonX, moonY, r := int(float64(w)*0.72), int(float64(he)*0.2), 56
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				img.Set(moonX+dx, moonY+dy, color.RGBA{245, 240, 214, 255})
			}
		}
	}
	// 远山两叠
	drawRidge(img, float64(he)*0.62, 90, color.RGBA{30, 26, 52, 255}, &rng, w)
	drawRidge(img, float64(he)*0.74, 130, color.RGBA{16, 14, 30, 255}, &rng, w)
	// 废弃钟楼剪影
	towerX := int(float64(w)*0.3) + int(seed%153)
	drawTower(img, towerX, int(float64(he)*0.78), 96, 300, color.RGBA{10, 9, 20, 255})
	// 边框与标注
	border := color.RGBA{222, 205, 160, 255}
	for i := 0; i < 3; i++ {
		for x := i; x < w-i; x++ {
			img.Set(x, i, border)
			img.Set(x, he-1-i, border)
		}
		for y := i; y < he-i; y++ {
			img.Set(i, y, border)
			img.Set(w-1-i, y, border)
		}
	}
	drawLabel(img, "LULU RPG - MOCK ART", 28, he-46, border)
	drawLabel(img, "configure IMG_MODEL for real images", 28, he-28, color.RGBA{160, 150, 130, 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func lerp(a, b color.RGBA, t float32) color.RGBA {
	return color.RGBA{
		R: uint8(float32(a.R) + (float32(b.R)-float32(a.R))*t),
		G: uint8(float32(a.G) + (float32(b.G)-float32(a.G))*t),
		B: uint8(float32(a.B) + (float32(b.B)-float32(a.B))*t),
		A: 255,
	}
}

func drawRidge(img *image.RGBA, baseY float64, amp int, c color.RGBA, rng *uint32, w int) {
	for x := 0; x < w; x++ {
		n := int(nextNoise(rng, 16)) - amp/2
		y := int(baseY) + n + amp/4
		for yy := y; yy < img.Bounds().Dy(); yy++ {
			img.Set(x, yy, c)
		}
	}
}

func nextNoise(rng *uint32, amp int) uint32 {
	*rng ^= *rng << 13
	*rng ^= *rng >> 17
	*rng ^= *rng << 5
	return *rng % uint32(amp)
}

func drawTower(img *image.RGBA, cx, baseY, w, h int, c color.RGBA) {
	for y := 0; y < h; y++ {
		half := w / 2 * (h - y) / h // 向上收窄
		for x := cx - half; x <= cx+half; x++ {
			img.Set(x, baseY-y, c)
		}
	}
	// 尖顶
	for y := 0; y < h/3; y++ {
		half := (w/2 - 2) * (h/3 - y) / (h / 3)
		for x := cx - half; x <= cx+half; x++ {
			img.Set(x, baseY-h-y, c)
		}
	}
	// 一扇亮着的小窗（呼应 Mock 剧情）
	wx, wy := cx-2, baseY-h/2
	for dy := 0; dy < 8; dy++ {
		for dx := 0; dx < 5; dx++ {
			img.Set(wx+dx, wy+dy, color.RGBA{255, 196, 96, 255})
		}
	}
}

func drawLabel(img *image.RGBA, text string, x, y int, c color.RGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}
