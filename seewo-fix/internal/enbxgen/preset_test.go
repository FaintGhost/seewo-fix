package enbxgen

import (
	"math"
	"regexp"
	"strconv"
	"testing"
)

var reNum = regexp.MustCompile(`-?\d+\.\d+`)

// samePath compares two path strings number by number, to 0.01 px.
func samePath(a, b string) bool {
	na, nb := reNum.FindAllString(a, -1), reNum.FindAllString(b, -1)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		x, _ := strconv.ParseFloat(na[i], 64)
		y, _ := strconv.ParseFloat(nb[i], 64)
		if math.Abs(x-y) > 0.01 {
			return false
		}
	}
	return true
}

// Paths 希沃 wrote for page 3 of the U7 deck.
func TestPresetPathsMatchSeewo(t *testing.T) {
	cases := []struct {
		prst string
		w, h float64
		want []string
	}{
		{"upArrow", 29.26666666666667, 28.799999999999997, []string{"M 0.000,14.400 L 14.633,0.000 L 29.267,14.400 L 21.950,14.400 L 21.950,28.800 L 7.317,28.800 L 7.317,14.400 z "}},
		{"ellipse", 195.60000000000002, 49.33333333333333, []string{"M 0.000,24.667 A97.800,24.667,0,0,1,97.800,-0.000 A97.800,24.667,0,0,1,195.600,24.667 A97.800,24.667,0,0,1,97.800,49.333 A97.800,24.667,0,0,1,0.000,24.667 z "}},
		{"rightArrow", 35.86666666666667, 28.733333333333334, []string{"M 0.000,7.183 L 21.500,7.183 L 21.500,0.000 L 35.867,14.367 L 21.500,28.733 L 21.500,21.550 L 0.000,21.550 z "}},
		{"curvedUpArrow", 395.0666666666667, 70.93333333333334, []string{
			"M 377.333,0.000 L 389.216,17.733 L 380.350,17.733 A184.233,70.933,0,0,1,193.100,70.851 A184.233,70.933,0,0,0,362.616,17.733 L 353.750,17.733  z",
			"M 184.233,70.933 A184.233,70.933,0,0,1,0.000,0.000 L 17.733,0.000 A184.233,70.933,0,0,0,201.967,70.933  z",
			"M 193.100,70.851 A184.233,70.933,0,0,0,362.616,17.733 L 353.750,17.733 L 377.333,0.000 L 389.216,17.733 L 380.350,17.733 A184.233,70.933,0,0,1,201.967,70.933 L 184.233,70.933 A184.233,70.933,0,0,1,0.000,0.000 L 17.733,0.000 A184.233,70.933,0,0,0,201.967,70.933 ",
		}},
	}
	for _, c := range cases {
		got, err := PresetPaths(c.prst, c.w, c.h, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%s: %d paths, want %d", c.prst, len(got), len(c.want))
		}
		for i := range got {
			if !samePath(got[i].Path, c.want[i]) {
				t.Errorf("%s path %d\n got %s\nwant %s", c.prst, i, got[i].Path, c.want[i])
			}
		}
	}
	cu, _ := PresetPaths("curvedUpArrow", 395, 70, nil)
	if cu[1].FillMode != "DarkenLess" || cu[2].IsFilled || !cu[2].IsStroke || cu[0].IsStroke {
		t.Errorf("fill/stroke flags: %+v", cu)
	}
}
