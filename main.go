package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
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
	Width         int
	Height        int
	Font          string
	Holiday       string
	Anniversary   []DateEntry
	DayCountSince *DateEntry
}

func (c *CalendarConfig) Merge(conf *CalendarConfig) {
	if conf == nil {
		return
	}
	if conf.Width != 0 {
		c.Width = conf.Width
	}
	if conf.Height != 0 {
		c.Height = conf.Height
	}
	if conf.Font != "" {
		c.Font = conf.Font
	}
	if conf.Holiday != "" {
		c.Holiday = conf.Holiday
	}
	if conf.Anniversary != nil {
		c.Anniversary = append(c.Anniversary, conf.Anniversary...)
	}
	if conf.DayCountSince != nil {
		c.DayCountSince = conf.DayCountSince
	}
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
var BackgroundColor = color.White
var TextColor = color.Black
var HolidayColor = color.RGBA{255, 0, 0, 255}

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
	return &Calendar{WeekLabels: DefaultWeeekLabels, IsDayOffFunc: DefaultIsDayOffFunc, Date: Today()}
}

func (c *Calendar) NextMonth() {
	c.Date = c.Date.AddDate(0, 1, -c.Date.Day()+1)
}

func DrawCalendar(img *gg.Context, c *Calendar, x, y, w, h int, afunc func(t time.Time) bool, label bool) {
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
				img.SetColor(HolidayColor)
			} else {
				img.SetColor(TextColor)
			}
			img.DrawString(d, float64(x+i*colsize), float64(y+rowsize-4))
		}
		y += rowsize + space
	}

	for d := 0; d < numdays; d++ {
		day := start.AddDate(0, 0, d)
		wd := int(day.Weekday())
		if c.IsDayOffFunc(day) {
			img.SetColor(HolidayColor)
		} else {
			img.SetColor(TextColor)
		}
		if selected == d {
			img.DrawRectangle(float64(x+wd*colsize+3), float64(y+1), float64(colsize-4), float64(rowsize-2))
			img.Fill()
			img.SetColor(BackgroundColor)
		}
		img.DrawString(fmt.Sprintf(" %2d", day.Day()), float64(x+wd*colsize), float64(y+rowsize-5))
		if afunc != nil && afunc(day) {
			img.SetColor(TextColor)
			img.DrawLine(float64(x+wd*colsize+8), float64(y+rowsize-4), float64(x+wd*colsize+colsize-4), float64(y+rowsize-4))
			img.Stroke()
		}
		if wd == 6 {
			y += rowsize
		}
	}
}

type DateEntry struct {
	Year  int
	Month int
	Day   int
	Label string
}

func (d *DateEntry) Key() [3]int {
	return [3]int{d.Year, int(d.Month), int(d.Day)}
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

func parsePath(p string) (string, time.Time, string) {
	name := path.Base(p)
	ext := path.Ext(name)
	name = name[0 : len(name)-len(ext)]
	date, err := time.ParseInLocation("2006-01-02", name, time.Now().Location())
	if err != nil {
		date = time.Now()
	}
	confName := path.Base(path.Dir(p))
	if confName == "" {
		confName = "default"
	}
	return confName, date, strings.ToLower(ext)
}

func loadHoliday(fname string) map[[3]int]string {
	ret := map[[3]int]string{}
	r, err := os.Open(fname)
	if err != nil {
		return ret
	}
	defer r.Close()
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		d := DateEntry{}
		d.UnmarshalText(scan.Bytes())
		if d.Year != 0 {
			ret[d.Key()] = d.Label
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

func writeImage(w io.Writer, confName string, date time.Time, ext string) error {
	confMap := map[string]*CalendarConfig{}
	toml.DecodeFile("./config.toml", &confMap)
	conf := CalendarConfig{Width: 800, Height: 480}
	conf.Merge(confMap["default"])
	if confName != "default" {
		conf.Merge(confMap[confName])
	}

	anniversary := map[[3]int]string{}
	for _, d := range conf.Anniversary {
		anniversary[d.Key()] = d.Label
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

	faces, err := loadFont(conf.Font, []float64{22, 72})
	if err != nil {
		log.Println(err)
		faces = append(faces, basicfont.Face7x13)
		faces = append(faces, basicfont.Face7x13)
	}

	dc := gg.NewContext(conf.Width, conf.Height)
	dc.SetColor(BackgroundColor)
	dc.DrawRectangle(0, 0, float64(conf.Width), float64(conf.Height))
	dc.Fill()

	holidays := loadHoliday(conf.Holiday)
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
	dc.SetColor(TextColor)
	dc.DrawString(fmt.Sprintf("%4d-%02d", cal.Date.Year(), cal.Date.Month()), 600, 40)
	DrawCalendar(dc, cal, 500, 40, 280, 200, anniversaryFunc, true)

	cal.NextMonth()
	dc.SetFontFace(faces[0])
	dc.SetColor(TextColor)
	dc.DrawString(fmt.Sprintf("%4d-%02d", cal.Date.Year(), cal.Date.Month()), 600, 270)
	DrawCalendar(dc, cal, 500, 270, 280, 200, anniversaryFunc, false)

	dc.SetFontFace(faces[1])

	px := float64(40)
	dc.SetColor(TextColor)
	s := fmt.Sprintf("%2d月%2d日(", date.Month(), date.Day())
	dc.DrawString(s, px, 260)
	sw, _ := dc.MeasureString(s)
	px += sw
	if date.Weekday() == 0 {
		dc.SetColor(HolidayColor)
	} else {
		dc.SetColor(TextColor)
	}
	s = JapaneseWeeekLabels[date.Weekday()]
	dc.DrawString(s, px, 260)
	sw, _ = dc.MeasureString(s)
	px += sw
	dc.SetColor(TextColor)
	s = ")"
	dc.DrawString(s, px, 260)

	since := conf.DayCountSince
	if since != nil {
		days := int(cal.SelectedDate.Sub(since.Date(cal.Date.Location())).Hours()) / 24
		dc.DrawString(fmt.Sprintf("%5d日", days), 40, 400)

		dc.SetFontFace(faces[0])
		dc.DrawString(fmt.Sprintf("since %d-%2d-%2d", since.Year, since.Month, since.Day), 160, 425)
	}

	rect := image.Rect(0, 0, conf.Width, conf.Height)
	if ext == ".png" {
		return png.Encode(w, dc.Image())
	}
	img := image.NewPaletted(rect, color.Palette{TextColor, HolidayColor, BackgroundColor})
	draw.Draw(img, rect, dc.Image(), image.Point{}, draw.Src)
	return gif.Encode(w, img, &gif.Options{NumColors: 3})
}

func handler(w http.ResponseWriter, r *http.Request) {
	b := new(bytes.Buffer)
	confName, date, ext := parsePath(r.URL.Path)
	offsetSec, _ := strconv.Atoi(r.FormValue("offset"))
	date = date.Add(time.Duration(offsetSec) * time.Second)
	err := writeImage(b, confName, date, ext)
	if err != nil {
		log.Fatal(err)
	}
	if ext == ".png" {
		w.Header().Add("content-type", "image/png")
	} else {
		w.Header().Add("content-type", "image/gif")
	}
	w.Header().Add("content-length", fmt.Sprint(b.Len()))
	now := time.Now()
	sec := now.Hour()*3600 + now.Minute()*60 + now.Second()
	w.Header().Add("x-expire-sec", fmt.Sprint(86400-sec))
	io.Copy(w, b)
}

// https://www.gnu.org/software/libc/manual/html_node/TZ-Variable.html
func applyPosixTZ() {
	fixedtz := os.Getenv("TZ")
	p := strings.IndexAny(fixedtz, "+-")
	if p < 0 {
		return
	}
	sign := -1
	if fixedtz[p] == '-' {
		sign = +1
	}
	var offsetHour, offsetMin, offsetSec int
	offset := strings.Split(fixedtz[p+1:], ":")
	offsetHour, _ = strconv.Atoi(offset[0])
	if len(offset) >= 2 {
		offsetMin, _ = strconv.Atoi(offset[1])
	}
	if len(offset) >= 3 {
		offsetSec, _ = strconv.Atoi(offset[2])
	}
	time.Local = time.FixedZone(fixedtz, sign*(offsetHour*3600+offsetMin*60+offsetSec))
}

func main() {
	applyPosixTZ()
	if len(os.Args) == 2 {
		out, err := os.Create(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
		confName, date, ext := parsePath(os.Args[1])
		err = writeImage(out, confName, date, ext)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		http.HandleFunc("/", handler)
		http.ListenAndServe(":8080", nil)
	}
}
