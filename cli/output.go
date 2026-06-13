package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"
)

// Format is an output rendering format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatCSV   Format = "csv"
	FormatTSV   Format = "tsv"
	FormatURL   Format = "url"
	FormatRaw   Format = "raw"
)

// Valid reports whether f is one of the supported formats.
func (f Format) Valid() bool {
	switch f {
	case FormatTable, FormatJSON, FormatJSONL, FormatCSV, FormatTSV, FormatURL, FormatRaw:
		return true
	}
	return false
}

// Renderer writes records in a chosen format.
type Renderer struct {
	format   Format
	fields   []string
	noHeader bool
	tmpl     string
	w        io.Writer
}

// NewRenderer builds a Renderer writing to w.
func NewRenderer(w io.Writer, format Format, fields []string, noHeader bool, tmpl string) *Renderer {
	return &Renderer{w: w, format: format, fields: fields, noHeader: noHeader, tmpl: tmpl}
}

// Render writes records (a slice of structs, or a single struct) in the configured format.
func (r *Renderer) Render(records any) error {
	rv := reflect.ValueOf(records)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		s := reflect.MakeSlice(reflect.SliceOf(rv.Type()), 1, 1)
		s.Index(0).Set(rv)
		rv = s
	}
	n := rv.Len()
	items := make([]any, n)
	for i := 0; i < n; i++ {
		items[i] = rv.Index(i).Interface()
	}

	if r.tmpl != "" {
		return r.renderTemplate(items)
	}
	switch r.format {
	case FormatJSON:
		return r.renderJSON(items)
	case FormatJSONL:
		return r.renderJSONL(items)
	case FormatCSV:
		return r.renderDelimited(items, ',')
	case FormatTSV:
		return r.renderDelimited(items, '\t')
	case FormatURL:
		return r.renderURL(items)
	case FormatRaw:
		return r.renderRaw(items)
	default:
		return r.renderTable(items)
	}
}

func (r *Renderer) renderJSON(items []any) error {
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	if len(items) == 1 {
		return enc.Encode(items[0])
	}
	return enc.Encode(items)
}

func (r *Renderer) renderJSONL(items []any) error {
	enc := json.NewEncoder(r.w)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderTemplate(items []any) error {
	t, err := template.New("row").Parse(r.tmpl)
	if err != nil {
		return fmt.Errorf("parse --template: %w", err)
	}
	for _, it := range items {
		if err := t.Execute(r.w, toAnyMap(it)); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(r.w)
	}
	return nil
}

func (r *Renderer) renderURL(items []any) error {
	for _, it := range items {
		m := toMap(it)
		if u, ok := m["url"]; ok && u != "" {
			_, _ = fmt.Fprintln(r.w, u)
		}
	}
	return nil
}

func (r *Renderer) renderRaw(items []any) error {
	cols := r.columns(items)
	for _, it := range items {
		m := toMap(it)
		vals := make([]string, 0, len(cols))
		for _, c := range cols {
			vals = append(vals, m[c])
		}
		_, _ = fmt.Fprintln(r.w, strings.Join(vals, " "))
	}
	return nil
}

func (r *Renderer) renderTable(items []any) error {
	if len(items) == 0 {
		return nil
	}
	cols := r.columns(items)
	tw := tabwriter.NewWriter(r.w, 0, 4, 2, ' ', 0)
	if !r.noHeader {
		_, _ = fmt.Fprintln(tw, strings.Join(upperAll(cols), "\t"))
	}
	for _, it := range items {
		m := toMap(it)
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = truncate(m[c], 60)
		}
		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

func (r *Renderer) renderDelimited(items []any, comma rune) error {
	if len(items) == 0 {
		return nil
	}
	cols := r.columns(items)
	cw := csv.NewWriter(r.w)
	cw.Comma = comma
	if !r.noHeader {
		if err := cw.Write(cols); err != nil {
			return err
		}
	}
	for _, it := range items {
		m := toMap(it)
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = m[c]
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func (r *Renderer) columns(items []any) []string {
	if len(r.fields) > 0 {
		return r.fields
	}
	if len(items) == 0 {
		return nil
	}
	return structJSONKeys(items[0])
}

func toAnyMap(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return v
	}
	return m
}

func toMap(v any) map[string]string {
	out := map[string]string{}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return out
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		key := jsonKey(f)
		if key == "-" {
			continue
		}
		out[key] = formatValue(rv.Field(i))
	}
	return out
}

func structJSONKeys(v any) []string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	var keys []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		key := jsonKey(f)
		if key == "-" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func jsonKey(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return f.Name
	}
	return name
}

func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts[i] = formatValue(v.Index(i))
		}
		return strings.Join(parts, ";")
	}
	return fmt.Sprintf("%v", v.Interface())
}

func upperAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(s)
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n-1]) + "..."
}
