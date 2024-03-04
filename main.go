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

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

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

func DrawCalendar(img *gg.Context, c *Calendar, x, y, w, h int, label bool) {
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
			img.DrawRectangle(float64(x+wd*colsize+1), float64(y+1), float64(colsize-2), float64(rowsize-2))
			img.Fill()
			img.SetColor(color.White)
		}
		img.DrawString(fmt.Sprintf(" %2d", day.Day()), float64(x+wd*colsize), float64(y+rowsize-5))
		if wd == 6 {
			y += rowsize
		}
	}
}

//go:embed holiday.csv
var holidayCSV string

func parseHoliday(s string) map[[3]int]string {
	scan := bufio.NewScanner(strings.NewReader(s))
	ret := map[[3]int]string{}

	for scan.Scan() {
		row := strings.Split(scan.Text(), ",")
		date := strings.Split(row[0], "/")
		if len(date) != 3 {
			date = strings.Split(row[0], "-")
		}
		if len(date) != 3 {
			continue
		}
		yy, _ := strconv.ParseInt(date[0], 10, 32)
		mm, _ := strconv.ParseInt(date[1], 10, 32)
		dd, _ := strconv.ParseInt(date[2], 10, 32)
		name := ""
		if len(row) > 2 {
			name = strings.TrimSpace(row[1])
		}
		ret[[3]int{int(yy), int(mm), int(dd)}] = name
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

func writeImage(w io.Writer, date time.Time) error {
	red := color.RGBA{255, 0, 0, 255}

	faces, err := loadFont("./ipag.ttf", []float64{22, 72})
	if err != nil {
		log.Println(err)
		faces = append(faces, basicfont.Face7x13)
		faces = append(faces, basicfont.Face7x13)
	}

	img := image.NewPaletted(image.Rect(0, 0, 800, 480), color.Palette{color.Black, red, color.White})

	dc := gg.NewContext(800, 480)
	dc.SetColor(color.White)
	dc.DrawRectangle(0, 0, 800, 480)
	dc.Fill()

	cal := NewCalendar()
	cal.Date = date
	cal.SelectedDate = date
	cal.IsDayOffFunc = func(d time.Time) bool {
		if DefaultIsDayOffFunc(d) {
			return true
		}
		holidays := parseHoliday(holidayCSV)
		_, isholiday := holidays[[3]int{d.Year(), int(d.Month()), int(d.Day())}]
		return isholiday
	}

	dc.SetFontFace(faces[0])
	dc.SetColor(color.Black)
	dc.DrawString(fmt.Sprintf("%4d-%02d", cal.Date.Year(), cal.Date.Month()), 600, 40)
	DrawCalendar(dc, cal, 500, 40, 280, 200, true)

	cal.Date = cal.Date.AddDate(0, 1, 0)
	dc.SetFontFace(faces[0])
	dc.SetColor(color.Black)
	dc.DrawString(fmt.Sprintf("%4d-%02d", cal.Date.Year(), cal.Date.Month()), 600, 270)
	DrawCalendar(dc, cal, 500, 270, 280, 200, false)

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

	draw.Draw(img, image.Rect(0, 0, 800, 480), dc.Image(), image.Point{}, draw.Src)
	return gif.Encode(w, img, &gif.Options{NumColors: 3})
}

func handler(w http.ResponseWriter, r *http.Request) {
	b := new(bytes.Buffer)
	name := path.Base(r.URL.Path)
	name = name[0 : len(name)-len(path.Ext(name))]
	date, err := time.ParseInLocation("2006-01-02", name, time.Now().Location())
	if err != nil {
		date = time.Now()
	}
	err = writeImage(b, date)
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
		err = writeImage(out, time.Now())
		if err != nil {
			log.Fatal(err)
		}
	} else {
		http.HandleFunc("/", handler)
		http.ListenAndServe(":8080", nil)
	}
}
