package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// maxShown cuts long values, such as lyrics, in the tags view.
const maxShown = 120

// printTags shows one file as the tag editor will: its format, revision and
// pictures, then the keys Navidrome reads, each with the fields it feeds,
// and the keys it never reads. It only reads the file, and needs neither the
// database nor the server.
func printTags(w io.Writer, path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	f, err := tagio.Read(path)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	sayf(tw, "File\t%s\n", f.Path)
	sayf(tw, "Format\t%s\n", describe(f))
	sayf(tw, "Size\t%d bytes, modified %s\n", f.Size, f.ModTime.UTC().Format(time.RFC3339))
	sayf(tw, "Revision\t%s\n", f.Revision())
	if len(f.Pictures) == 0 {
		sayln(tw, "Pictures\tnone")
	}
	for i, p := range f.Pictures {
		label := ""
		if i == 0 {
			label = "Pictures"
		}
		sayf(tw, "%s\t%d: %s\n", label, i+1, strings.Join(nonEmpty(p.Type, p.MIME, p.Description), ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	standard, extended := f.Groups()
	sayf(w, "\nStandard: %s Navidrome reads\n", keys(len(standard)))
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, k := range standard {
		var names []string
		for _, fd := range tagio.FieldsOf(f.Format, k) {
			names = append(names, fd.Name)
		}
		field := strings.Join(names, ", ")
		if f.Format.ReadOnly(k) {
			field += " (read-only)"
		}
		printValues(tw, []string{k, field}, f.Tags[k])
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	sayf(w, "\nExtended: %s Navidrome never reads\n", keys(len(extended)))
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, k := range extended {
		printValues(tw, []string{k}, f.Tags[k])
	}
	return tw.Flush()
}

func keys(n int) string {
	if n == 1 {
		return "1 key"
	}
	return strconv.Itoa(n) + " keys"
}

// printValues writes a key's values one per line, with its cells only on
// the first.
func printValues(w io.Writer, cells []string, vs []string) {
	for i, v := range vs {
		row := cells
		if i > 0 {
			row = make([]string, len(cells))
		}
		sayf(w, "  %s\t%s\n", strings.Join(row, "\t"), shorten(v))
	}
}

func shorten(v string) string {
	v = strings.ReplaceAll(v, "\r\n", "\n")
	n := len([]rune(v))
	v = strings.ReplaceAll(v, "\n", `\n`)
	if r := []rune(v); len(r) > maxShown {
		return string(r[:maxShown]) + "… (" + strconv.Itoa(n) + " characters)"
	}
	return v
}

// describe gives the format in words, such as "FLAC 24-bit 96 kHz, 2
// channels, 4:05".
func describe(f tagio.File) string {
	a := f.Audio
	parts := []string{f.Format.Label}
	if a.Codec != "" {
		parts[0] += " (" + a.Codec + ")"
	}
	q := ""
	if a.BitDepth > 0 {
		q = fmt.Sprintf("%d-bit ", a.BitDepth)
	}
	q += strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", float64(a.SampleRate)/1000), "0"), ".") + " kHz"
	if a.BitDepth == 0 && a.BitRate > 0 {
		q += fmt.Sprintf(" %d kbit/s", a.BitRate)
	}
	secs := int(a.Length.Round(time.Second) / time.Second)
	return fmt.Sprintf("%s %s, %d channels, %d:%02d", parts[0], q, a.Channels, secs/60, secs%60)
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
