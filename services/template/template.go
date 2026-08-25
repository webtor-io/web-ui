package template

import (
	"bytes"
	"container/list"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
	log "github.com/sirupsen/logrus"
	"github.com/yargevad/filepathx"
)

type FuncMap = template.FuncMap

// HTML is html/template's HTML, re-exported for the same reason FuncMap is:
// a handler that has to hand a view a value already known to be markup
// should not have to import html/template alongside this package and alias
// one of the two to dodge the name clash.
type HTML = template.HTML

type View struct {
	Name         string
	Path         string
	LayoutPath   string
	Layout       string
	LayoutBody   string
	Partials     []string
	Funcs        FuncMap
	once         sync.Once
	err          error
	re           multitemplate.Renderer
	templateName string
	mux          sync.Mutex
}

func (s *View) makeTemplate() (t *template.Template, err error) {
	var templates []string

	// Create a pointer to hold the parsed template for the dynamicTemplate function
	var parsedTemplate *template.Template

	// Build the full FuncMap upfront so it's available for ALL parse calls,
	// including LayoutBody which is parsed from a string (not a file).
	funcs := make(FuncMap)
	for k, v := range s.Funcs {
		funcs[k] = v
	}
	funcs["dynamicTemplate"] = func(name string, data any) (template.HTML, error) {
		if parsedTemplate == nil {
			return "", errors.New("template not yet parsed")
		}
		var buf bytes.Buffer
		if err := parsedTemplate.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil
	}

	if s.LayoutBody != "" {
		t, err = template.New(s.Name).Funcs(funcs).Parse(s.LayoutBody)
		if err != nil {
			return nil, err
		}
	} else if s.Layout != "" {
		templates = append(templates, s.LayoutPath)
		t = template.New(filepath.Base(s.LayoutPath))
	} else {
		t = template.New(filepath.Base(s.Path))
	}
	templates = append(templates, s.Path)
	templates = append(templates, s.Partials...)

	parsedTemplate, err = t.Funcs(funcs).ParseFiles(templates...)
	return parsedTemplate, err
}

func (s *View) makeTemplateName() string {
	name := s.Name
	if s.LayoutBody != "" {
		hash := md5.Sum([]byte(s.LayoutBody))
		hashStr := hex.EncodeToString(hash[:])
		name += "_" + hashStr

	} else if s.Layout != "" {
		name += "_" + s.Layout
	}
	return name
}

func (s *View) Render() (string, error) {
	f := func() {
		s.templateName = s.makeTemplateName()
		var t *template.Template
		t, s.err = s.makeTemplate()
		if s.err != nil {
			return
		}
		s.mux.Lock()
		defer s.mux.Unlock()
		s.re.Add(s.templateName, t)
	}
	if gin.IsDebugging() {
		f()
	} else {
		s.once.Do(f)
	}
	return s.templateName, s.err
}

type Context struct {
	Data any
	Err  error
}

func NewContext(_ *gin.Context, obj any, err error) any {
	return &Context{
		Data: obj,
		Err:  err,
	}
}

// defaultLayoutCacheCap bounds how many distinct request-supplied layout
// bodies we keep parsed at once.
//
// Layout bodies arrive as raw client headers (`X-Layout`, `X-Update-*`), so
// the set of possible values is decided by the caller, not by us. They used to
// be retained for the process lifetime — one `*View` appended to Manager.views
// plus one fully-parsed template set in the shared renderer per distinct value,
// with no eviction. Retained heap therefore grew with the number of distinct
// values a client chose to send, which is not a bound we control.
//
// Our own frontend emits ~10 distinct values in total — the
// `data-async-layout` and `data-async-update-*` directives baked into the
// templates, plus `{{ template "main" . }}` from jobs/scripts/action.go. A cap
// of 32 leaves generous headroom for real traffic while making the worst case
// bounded: values beyond the cap now churn the LRU (costing a re-parse)
// instead of accumulating.
const defaultLayoutCacheCap = 32

// maxConcurrentLayoutBuilds caps parallel parses of layout template sets.
// Cache misses are rare in normal operation (the handful of real layout
// directives warm up on first use), so this only ever bites a caller
// generating fresh bodies — which is the case worth bounding.
const maxConcurrentLayoutBuilds = 4

type layoutEntry struct {
	key  string
	tmpl *template.Template
}

type Manager[K GinContext] struct {
	re       multitemplate.Renderer
	funcs    FuncMap
	layouts  []string
	partials []string
	views    []*View
	mux      sync.Mutex
	base     string

	// layoutCache/layoutLRU hold templates built from request-supplied layout
	// bodies. Deliberately NOT the shared multitemplate renderer: that is a
	// bare map[string]*template.Template which gin reads (Instance) on every
	// c.HTML with no lock, so writing to it at request time is a latent
	// "concurrent map read and map write" fatal error, and entries there can
	// never be evicted safely because gin resolves the name after we return.
	// Owning the map lets us hold mux across lookup and eviction.
	layoutCache map[string]*list.Element
	layoutLRU   *list.List
	layoutCap   int

	// buildSem caps how many layout template sets are parsed at once. Its own
	// sync.Once rather than the constructor so a zero-value Manager (tests)
	// still works. Not guarded by mux: builds run outside the lock on purpose.
	buildSem     chan struct{}
	buildSemOnce sync.Once
}

func NewManager[K GinContext](re multitemplate.Renderer) *Manager[K] {
	return &Manager[K]{
		re:        re,
		funcs:     FuncMap{},
		base:      "templates/",
		layoutCap: defaultLayoutCacheCap,
	}
}

func fileNameWithoutExt(fileName string) string {
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}

func (s *Manager[K]) MustRegisterViews(pattern string) *Manager[K] {
	return Must(s.RegisterViews(pattern))
}

func (s *Manager[K]) RegisterViews(pattern string) (m *Manager[K], err error) {
	m = s
	views, err := s.getFiles("views/" + pattern)
	if err != nil {
		return
	}
	layouts, err := s.GetLayouts()
	if err != nil {
		return
	}
	partials, err := s.GetPartials()
	if err != nil {
		return
	}
	for _, v := range views {
		s.views = append(s.views, s.makeView(v, "", partials, s.funcs))
		for _, l := range layouts {
			s.views = append(s.views, s.makeView(v, l, partials, s.funcs))
		}
	}

	return
}

func (s *Manager[K]) getFiles(pattern string) ([]string, error) {
	g, err := filepathx.Glob(s.base + pattern)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, l := range g {
		f, _ := os.Stat(l)
		if f.IsDir() {
			continue
		}
		res = append(res, l)
	}
	return res, nil
}

func (s *Manager[K]) GetLayouts() ([]string, error) {
	if s.layouts != nil {
		return s.layouts, nil
	}
	layouts, err := s.getFiles("layouts/**/*")
	if err != nil {
		return nil, err
	}

	s.layouts = layouts
	return layouts, nil
}

func (s *Manager[K]) GetPartials() ([]string, error) {
	if s.partials != nil {
		return s.partials, nil
	}
	partials, err := s.getFiles("partials/**")
	if err != nil {
		return nil, err
	}
	s.partials = partials
	return partials, nil
}

func (s *Manager[K]) makeView(view string, layout string, partials []string, funcs FuncMap) *View {
	lName := fileNameWithoutExt(strings.TrimPrefix(layout, s.base+"layouts/"))
	vName := fileNameWithoutExt(strings.TrimPrefix(view, s.base+"views/"))
	return &View{
		re:         s.re,
		Name:       vName,
		Path:       view,
		LayoutPath: layout,
		Layout:     lName,
		Partials:   partials,
		Funcs:      funcs,
	}
}

func (s *Manager[K]) makeViewWithLayoutBody(mv *View, layout string) *View {
	cv := s.makeView(mv.Path, mv.LayoutPath, mv.Partials, mv.Funcs)
	cv.LayoutBody = layout
	return cv
}

func (s *Manager[K]) WithFuncs(f FuncMap) *Manager[K] {
	for k, v := range f {
		s.funcs[k] = v
	}
	return s
}

func (s *Manager[K]) firstToLower(in string) string {
	r, size := utf8.DecodeRuneInString(in)
	if r == utf8.RuneError && size <= 1 {
		return in
	}
	lc := unicode.ToLower(r)
	if r == lc {
		return in
	}
	return string(lc) + in[size:]
}

func (s *Manager[K]) WithHelper(h any) *Manager[K] {
	hv := reflect.ValueOf(h)
	ht := hv.Type()
	for i := 0; i < ht.NumMethod(); i++ {
		// Register the bound method value directly as a typed FuncMap entry.
		// html/template already invokes FuncMap funcs via reflection, so the
		// previous `func(args ...any) any` wrapper added a SECOND reflection
		// layer that allocated a fresh []any (append) + []reflect.Value +
		// per-arg reflect.ValueOf on EVERY helper call in EVERY render. Under
		// a burst of concurrent renders that churn was the top alloc_space
		// consumer (reflect.Value.call) and could overshoot GOMEMLIMIT toward
		// the container limit. A bound method value carries its concrete typed
		// signature, so the engine calls it directly in a single reflection
		// pass with no per-call wrapper allocation. Safe: every helper method
		// returns a single value (no (T, error) methods), so this preserves
		// the old `.Call(...)[0]`-only return behaviour.
		s.funcs[s.firstToLower(ht.Method(i).Name)] = hv.Method(i).Interface()
	}
	return s
}

func (s *Manager[K]) Init() error {
	for _, v := range s.views {
		_, err := s.renderView(v)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Manager[K]) RenderViewByNameAndLayout(name string, layout string) (string, error) {
	for _, v := range s.views {
		if v.Name == name && v.Layout == layout {
			return s.renderView(v)
		}
	}
	return "", errors.New("view not found")
}

// ExecuteLayoutBody renders `name` wrapped in a caller-supplied layout body
// and returns the output directly.
//
// It replaces the old RenderViewByNameAndLayoutBody, which returned a template
// *name* and required the template to stay registered in the shared renderer
// for the caller to look it up. Every call site executed immediately anyway,
// so the registry round-trip bought nothing and cost an unbounded, un-evictable
// cache — see defaultLayoutCacheCap.
func (s *Manager[K]) ExecuteLayoutBody(name string, layoutBody string, obj any) (string, error) {
	t, err := s.layoutTemplate(name, layoutBody)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err = t.Execute(&b, obj); err != nil {
		return "", err
	}
	return b.String(), nil
}

// layoutTemplate returns the parsed template for (name, layoutBody), building
// it on a miss. The build runs OUTSIDE the lock: parsing the whole template
// set is the expensive part and holding mux across it would serialise every
// concurrent AJAX render. Two goroutines racing on the same cold key both
// build; the loser drops its copy and takes the winner's.
func (s *Manager[K]) layoutTemplate(name string, layoutBody string) (*template.Template, error) {
	// Key on a digest, not the body itself: the body is a client-sized header
	// and using it as a map key would retain the whole string.
	//
	// SHA-256, not MD5, precisely because the body is caller-supplied and the
	// cache is shared across everyone. Under a collision-prone digest a caller
	// able to craft a second preimage for one of our own layout directives
	// would get its template served to every other request that asks for the
	// real one — the digest is the only thing standing between two different
	// templates here.
	hash := sha256.Sum256([]byte(layoutBody))
	key := name + "_" + hex.EncodeToString(hash[:])

	s.mux.Lock()
	if el, ok := s.layoutCache[key]; ok {
		s.layoutLRU.MoveToFront(el)
		t := el.Value.(*layoutEntry).tmpl
		s.mux.Unlock()
		return t, nil
	}
	s.mux.Unlock()

	t, err := s.buildLayoutTemplate(name, layoutBody)
	if err != nil {
		return nil, err
	}

	s.mux.Lock()
	defer s.mux.Unlock()
	if el, ok := s.layoutCache[key]; ok {
		s.layoutLRU.MoveToFront(el)
		return el.Value.(*layoutEntry).tmpl, nil
	}
	if s.layoutCache == nil {
		s.layoutCache = map[string]*list.Element{}
		s.layoutLRU = list.New()
	}
	if s.layoutCap <= 0 {
		s.layoutCap = defaultLayoutCacheCap
	}
	s.layoutCache[key] = s.layoutLRU.PushFront(&layoutEntry{key: key, tmpl: t})
	for s.layoutLRU.Len() > s.layoutCap {
		oldest := s.layoutLRU.Back()
		if oldest == nil {
			break
		}
		s.layoutLRU.Remove(oldest)
		delete(s.layoutCache, oldest.Value.(*layoutEntry).key)
	}
	return t, nil
}

// buildLayoutTemplate parses a fresh template set for the given layout body.
// The result is intentionally NOT added to Manager.views or to the shared
// renderer, so an evicted entry becomes garbage with no dangling references.
//
// s.views is append-only during RegisterViews (startup) and immutable once
// serving starts, so reading it here without the lock is safe.
//
// Builds are throttled by buildSem. Parsing a whole template set is the one
// genuinely expensive, allocation-heavy step on this path, and cache misses are
// exactly what a caller sending never-before-seen layout bodies produces. Left
// unthrottled, concurrent misses would multiply that cost by the number of
// in-flight requests — trading the retention problem this cache exists to fix
// for a transient one. Steady-state traffic is all cache hits and never reaches
// the semaphore.
func (s *Manager[K]) buildLayoutTemplate(name string, layoutBody string) (*template.Template, error) {
	s.buildSemOnce.Do(func() {
		s.buildSem = make(chan struct{}, maxConcurrentLayoutBuilds)
	})
	s.buildSem <- struct{}{}
	defer func() { <-s.buildSem }()

	var mv *View
	for _, v := range s.views {
		if v.Name == name && v.LayoutBody == "" {
			mv = v
		}
	}
	if mv == nil {
		return nil, errors.New("view not found")
	}
	// makeTemplate ignores LayoutPath when LayoutBody is set, so any view
	// registered under this name yields the same parsed set (path + partials).
	return s.makeViewWithLayoutBody(mv, layoutBody).makeTemplate()
}

func (s *Manager[K]) renderView(v *View) (string, error) {
	name, err := v.Render()
	if err != nil {
		return "", err
	}
	return name, nil
}

type Template[K GinContext] struct {
	name       string
	layoutBody string
	layout     string
	tm         *Manager[K]
}

type GinContext interface {
	GetGinContext() *gin.Context
}

func (s *Template[K]) HTML(code int, ctx K) {
	c := ctx.GetGinContext()
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		var buf bytes.Buffer
		if c.GetHeader("X-Layout") != "" {
			mainTpl := s.tm.Build(s.name).WithLayoutBody(c.GetHeader("X-Layout"))
			str, rerr := mainTpl.ToString(ctx)
			if rerr != nil {
				log.WithError(rerr).Error("failed to render main view")
				c.String(http.StatusInternalServerError, "Internal Server Error")
				return
			}
			writeFragment(&buf, "main", str)
		}
		for headerName, vals := range c.Request.Header {
			if !strings.HasPrefix(headerName, "X-Update-") {
				continue
			}
			tpl := s.tm.Build(s.name).WithLayoutBody(vals[0])
			str, rerr := tpl.ToString(ctx)
			if rerr != nil {
				log.WithError(rerr).Error("failed to render update template to string")
				c.String(http.StatusInternalServerError, "Internal Server Error")
				return
			}
			fragName := strings.ToLower(strings.TrimPrefix(headerName, "X-Update-"))
			writeFragment(&buf, fragName, str)
		}
		c.Data(code, "text/html; charset=utf-8", buf.Bytes())
		return
	}
	name, rerr := s.tm.RenderViewByNameAndLayout(s.name, s.layout)
	if rerr != nil {
		log.WithError(rerr).Error("failed to render view by name and layout")
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	c.HTML(code, name, ctx)
}

// writeFragment emits one <template data-async-fragment="name">…</template>
// block. Fragment names come from a fixed allow-list driven by
// data-async-update-* attributes (lowercased ASCII), so they're safe to
// embed without escaping.
func writeFragment(buf *bytes.Buffer, name, body string) {
	buf.WriteString(`<template data-async-fragment="`)
	buf.WriteString(name)
	buf.WriteString(`">`)
	buf.WriteString(body)
	buf.WriteString(`</template>`)
}

func (s *Template[K]) ToString(obj K) (res string, err error) {
	// Request-supplied layout bodies are rendered straight from the bounded
	// LRU and never touch the shared renderer.
	if s.layoutBody != "" {
		return s.tm.ExecuteLayoutBody(s.name, s.layoutBody, obj)
	}
	v, err := s.tm.RenderViewByNameAndLayout(s.name, s.layout)
	if err != nil {
		return
	}
	var b bytes.Buffer
	re, _ := s.tm.re.Instance(v, obj).(render.HTML)
	err = re.Template.Execute(&b, re.Data)
	if err != nil {
		return
	}
	res = b.String()
	return
}

func (s *Manager[K]) Build(name string) *Template[K] {
	return &Template[K]{
		name: name,
		tm:   s,
	}
}

func (s *Template[K]) WithLayout(name string) *Template[K] {
	s.layout = name
	return s
}

func (s *Template[K]) WithLayoutBody(body string) *Template[K] {
	s.layoutBody = body
	return s
}

func Must[K GinContext](m *Manager[K], err error) *Manager[K] {
	if err != nil {
		panic(err)
	}
	return m
}

type BuilderWithLayout[K GinContext] struct {
	tm     *Manager[K]
	layout string
}

func (s *BuilderWithLayout[K]) Build(name string) *Template[K] {
	return s.tm.Build(name).WithLayout(s.layout)
}

type Builder[K GinContext] interface {
	Build(name string) *Template[K]
}

func (s *Manager[K]) WithLayout(name string) *BuilderWithLayout[K] {
	return &BuilderWithLayout[K]{
		tm:     s,
		layout: name,
	}

}
