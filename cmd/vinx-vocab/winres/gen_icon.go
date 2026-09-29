//go:build ignore

// 生成 Windows 可执行文件图标 icon.png（256×256）：品牌主色圆角方块 + 白色「V」+ 强调色圆点（对应页面上的 Vinx·Vocab 字标）。
// 用法：go run gen_icon.go（在本目录）；之后 make cross 会用 go-winres 把它和版本信息写进 exe。
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const size = 256

var (
	primary = color.NRGBA{0x0f, 0x6e, 0x66, 0xff}
	accent  = color.NRGBA{0xc2, 0x41, 0x0c, 0xff}
	white   = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// 4×4 超采样抗锯齿
func main() {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					c, ok := shade(px, py)
					if ok {
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
						a += 255
					}
				}
			}
			n := float64(ss * ss)
			if a == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(r / (a / 255)), uint8(g / (a / 255)), uint8(b / (a / 255)), uint8(a / n)})
		}
	}
	f, err := os.Create("icon.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func shade(x, y float64) (color.NRGBA, bool) {
	if !inRoundRect(x, y, 8, 8, size-8, size-8, 48) {
		return color.NRGBA{}, false
	}
	// 强调色圆点（右下）
	if math.Hypot(x-196, y-190) <= 20 {
		return accent, true
	}
	// 「V」：两条粗线段
	if distSeg(x, y, 64, 62, 126, 196) <= 21 || distSeg(x, y, 188, 62, 126, 196) <= 21 {
		return white, true
	}
	return primary, true
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Min(math.Max(x, x0+r), x1-r)
	cy := math.Min(math.Max(y, y0+r), y1-r)
	return math.Hypot(x-cx, y-cy) <= r
}

func distSeg(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}
