package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

type CalendarConfig struct {
	Font          string
	Holiday       string
	Anniversary   []DateEntry
	DayCountSince *DateEntry
}

type Config map[string]CalendarConfig

func (c Config) Font(kind string) string {
	if conf, ok := c[kind]; ok && conf.Font != "" {
		return conf.Font
	}
	if conf, ok := c["default"]; ok && conf.Font != "" {
		return conf.Font
	}
	return "./ipag.ttf"
}

func (c Config) Anniversary(kind string) []DateEntry {
	if conf, ok := c[kind]; ok && conf.Anniversary != nil {
		return conf.Anniversary
	}
	if conf, ok := c["default"]; ok && conf.Anniversary != nil {
		return conf.Anniversary
	}
	return nil
}

func (c Config) DayCountSince(kind string) *DateEntry {
	if conf, ok := c[kind]; ok && conf.DayCountSince != nil {
		return conf.DayCountSince
	}
	if conf, ok := c["default"]; ok && conf.DayCountSince != nil {
		return conf.DayCountSince
	}
	return nil
}

// TODO github.com/binzume/gocal
type Calendar struct {
	WeekLabels   []string
	MonthLabels  []string
	Date         time.Time
	IsDayOffFunc func(t time.Time) bool
	LinkFunc     func(t time.Time) string
	SelectedDate time.Time
}

var DefaultWeeekLabels = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var JapaneseWeeekLabels = []string{"日", "月", "火", "水", "木", "金", "土"}

func DefaultIsDayOffFunc(t time.Time) bool {
	return t.Weekday() == time.Sunday || t.Weekday() == time.Saturday
}

func ToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
func Today() time.Time {
	return ToDate(time.Now())
}

func NewCalendar() *Calendar {
	return &Calendar{WeekLabels: DefaultWeeekLabels, IsDayOffFunc: DefaultIsDayOffFunc, Date: time.Now()}
}

func (c *Calendar) NextMonth() {
	c.Date = c.Date.AddDate(0, 1, -c.Date.Day()+1)
}

func DrawCalendar(img *gg.Context, c *Calendar, x, y, w, h int, afunc func(t time.Time) bool, label bool) {
	red := color.RGBA{255, 0, 0, 255}
	wc := len(c.WeekLabels)
	start := ToDate(c.Date.AddDate(0, 0, -c.Date.Day()+1))
	last := start.AddDate(0, 1, 0)
	numdays := int(last.Sub(start).Hours()) / 24
	selected := int(c.SelectedDate.Sub(start).Hours()) / 24

	space := 2
	colsize := w / wc
	rowsize := (h - space) / 7

	if label {
		for i, d := range c.WeekLabels {
			if i == 0 {
				img.SetColor(red)
			} else {
				img.SetColor(color.Black)
			}
			img.DrawString(d, float64(x+i*colsize), float64(y+rowsize-4))
		}
		y += rowsize + space
	}

	for d := 0; d < numdays; d++ {
		day := start.AddDate(0, 0, d)
		wd := int(day.Weekday())
		if c.IsDayOffFunc(day) {
			img.SetColor(red)
		} else {
			img.SetColor(color.Black)
		}
		if selected == d {
			img.DrawRectangle(float64(x+wd*colsize+3), float64(y+1), float64(colsize-4), float64(rowsize-2))
			img.Fill()
			img.SetColor(color.White)
		}
		img.DrawString(fmt.Sprintf(" %2d", day.Day()), float64(x+wd*colsize), float64(y+rowsize-5))
		if afunc != nil && afunc(day) {
			img.SetColor(color.Black)
			img.DrawLine(float64(x+wd*colsize+8), float64(y+rowsize-4), float64(x+wd*colsize+colsize-4), float64(y+rowsize-4))
			img.Stroke()
		}
		if wd == 6 {
			y += rowsize
		}
	}
}

//go:embed holiday.csv
var holidayCSV string

type DateEntry struct {
	Year  int
	Month int
	Day   int
	Label string
}

func (d *DateEntry) Date(l *time.Location) time.Time {
	return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, l)
}

func (d *DateEntry) UnmarshalText(ent []byte) error {
	row := strings.Split(string(ent), ",")
	date := strings.Split(row[0], "/")
	if len(date) != 3 {
		date = strings.Split(row[0], "-")
	}
	if len(date) != 3 {
		return nil
	}
	yy, _ := strconv.ParseInt(date[0], 10, 32)
	mm, _ := strconv.ParseInt(date[1], 10, 32)
	dd, _ := strconv.ParseInt(date[2], 10, 32)
	if date[0] == "*" {
		yy = -1
	}
	if date[1] == "*" {
		mm = -1
	}
	d.Year = int(yy)
	d.Month = int(mm)
	d.Day = int(dd)
	if len(row) >= 2 {
		d.Label = strings.TrimSpace(row[1])
	}
	return nil
}

func parseEntry(ent []byte) *DateEntry {
	d := DateEntry{}
	d.UnmarshalText(ent)
	if d.Year == 0 {
		return nil
	}
	return &d
}

func parsePath(p string) (string, time.Time) {
	name := path.Base(p)
	name = name[0 : len(name)-len(path.Ext(name))]
	date, err := time.ParseInLocation("2006-01-02", name, time.Now().Location())
	if err != nil {
		date = time.Now()
	}
	kind := path.Base(path.Dir(p))
	if kind == "" {
		kind = "default"
	}
	return kind, date
}

func parseHoliday(s string) map[[3]int]string {
	scan := bufio.NewScanner(strings.NewReader(s))
	ret := map[[3]int]string{}
	for scan.Scan() {
		if d := parseEntry(scan.Bytes()); d != nil {
			ret[[3]int{d.Year, d.Month, d.Day}] = d.Label
		}
	}
	return ret
}

func loadFont(path string, sizes []float64) ([]font.Face, error) {
	ttf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	font_, err := truetype.Parse(ttf)
	if err != nil {
		return nil, err
	}

	faces := []font.Face{}
	for _, sz := range sizes {
		faces = append(faces, truetype.NewFace(font_, &truetype.Options{
			Size: sz,
		}))
	}

	return faces, nil
}

func writeImage(w io.Writer, kind string, date time.Time) error {
	conf := Config{}
	toml.DecodeFile("./config.toml", &conf)

	anniversary := map[[3]int]string{}
	for _, d := range conf.Anniversary(kind) {
		if d.Year != 0 {
			anniversary[[3]int{d.Year, d.Month, d.Day}] = d.Label
		}
	}
	anniversaryFunc := func(day time.Time) bool {
		if _, ok := anniversary[[3]int{day.Year(), int(day.Month()), day.Day()}]; ok {
			return true
		}
		if _, ok := anniversary[[3]int{-1, int(day.Month()), day.Day()}]; ok {
			return true
		}
		if _, ok := anniversary[[3]int{-1, -1, day.Day()}]; ok {
			return true
		}
		return false
	}

	red := color.RGBA{255, 0, 0, 255}

	faces, err := loadFont(conf.Font(kind), []float64{22, 72})
	if err != nil {
		log.Println(err)
		faces = append(faces, basicfont.Face7x13)
		faces = append(faces, basicfont.Face7x13)
	}

	dc := gg.NewContext(800, 480)
	dc.SetColor(color.White)
	dc.DrawRectangle(0, 0, 800, 480)
	dc.Fill()

	holidays := parseHoliday(holidayCSV)
	cal := NewCalendar()
	cal.Date = date
	cal.SelectedDate = date
	cal.IsDayOffFunc = func(d time.Time) bool {
		if DefaultIsDayOffFunc(d) {
			return true
		}
		_, isholiday := holidays[[3]int{d.Year(), int(d.Month()), int(d.Day())}]
		return isholiday
	}

	dc.SetFontFace(faces[0])
	dc.SetColor(color.Black)
	dc.DrawString(fmt.Sprintf("%4d-%02d", cal.Date.Year(), cal.Date.Month()), 600, 40)
	DrawCalendar(dc, cal, 500, 40, 280, 200, anniversaryFunc, true)

	cal.NextMonth()
	dc.SetFontFace(faces[0])
	dc.SetColor(color.Black)
	dc.DrawString(fmt.Sprintf("%4d-%02d", cal.Date.Year(), cal.Date.Month()), 600, 270)
	DrawCalendar(dc, cal, 500, 270, 280, 200, anniversaryFunc, false)

	dc.SetFontFace(faces[1])

	px := float64(40)
	dc.SetColor(color.Black)
	s := fmt.Sprintf("%2d月%2d日(", date.Month(), date.Day())
	dc.DrawString(s, px, 260)
	sw, _ := dc.MeasureString(s)
	px += sw
	if date.Weekday() == 0 {
		dc.SetColor(red)
	} else {
		dc.SetColor(color.Black)
	}
	s = JapaneseWeeekLabels[date.Weekday()]
	dc.DrawString(s, px, 260)
	sw, _ = dc.MeasureString(s)
	px += sw
	dc.SetColor(color.Black)
	s = ")"
	dc.DrawString(s, px, 260)

	since := conf.DayCountSince(kind)
	if since != nil {
		days := int(cal.SelectedDate.Sub(since.Date(cal.Date.Location())).Hours()) / 24
		dc.DrawString(fmt.Sprintf("%5d日", days), 40, 400)

		dc.SetFontFace(faces[0])
		dc.DrawString(fmt.Sprintf("since %d-%2d-%2d", since.Year, since.Month, since.Day), 160, 425)
	}

	img := image.NewPaletted(image.Rect(0, 0, 800, 480), color.Palette{color.Black, red, color.White})
	draw.Draw(img, image.Rect(0, 0, 800, 480), dc.Image(), image.Point{}, draw.Src)
	return gif.Encode(w, img, &gif.Options{NumColors: 3})
}

func handler(w http.ResponseWriter, r *http.Request) {
	b := new(bytes.Buffer)
	kind, date := parsePath(r.URL.Path)
	err := writeImage(b, kind, date)
	if err != nil {
		log.Fatal(err)
	}
	w.Header().Add("content-type", "image/gif")
	w.Header().Add("content-length", fmt.Sprint(b.Len()))
	now := time.Now()
	sec := now.Hour()*3600 + now.Minute()*60 + now.Second()
	w.Header().Add("x-expire-sec", fmt.Sprint(86400-sec))
	io.Copy(w, b)
}

func main() {
	fixedtz := os.Getenv("FIXED_TZ") // ex: JST-9
	if p := strings.LastIndexAny(fixedtz, "+-"); p >= 0 {
		offset, _ := strconv.Atoi(fixedtz[p:])
		time.Local = time.FixedZone(fixedtz, -offset*3600)
	}
	if len(os.Args) == 2 {
		out, err := os.Create(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
		kind, date := parsePath(os.Args[1])
		err = writeImage(out, kind, date)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		http.HandleFunc("/", handler)
		http.ListenAndServe(":8080", nil)
	}
}
