package helpers

import (
	"fmt"
	"math"
)

// Bytes renders a size for people: "1.0 MB", "512 KB" — base 1024 with the
// KB/MB/GB labels Chrome and Windows Explorer use (macOS Finder counts by
// 1000; we follow the browser the page is shown in). The space before the
// unit is a no-break space (U+00A0) so a narrow screen never splits the number
// from its unit; every number-plus-unit in the UI follows the same rule
// (docs/i18n.md, "Numbers and units").
func Bytes(s uint64) string {
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	return humanateBytes(s, 1024, sizes)
}

func humanateBytes(s uint64, base float64, sizes []string) string {
	if s < 10 {
		return fmt.Sprintf("%d\u00a0B", s)
	}
	e := math.Floor(logn(float64(s), base))
	suffix := sizes[int(e)]
	val := math.Floor(float64(s)/math.Pow(base, e)*10+0.5) / 10
	f := "%.0f\u00a0%s"
	if val < 10 {
		f = "%.1f\u00a0%s"
	}

	return fmt.Sprintf(f, val, suffix)
}

func logn(n, b float64) float64 {
	return math.Log(n) / math.Log(b)
}
