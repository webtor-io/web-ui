package template

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"

	"github.com/gin-contrib/multitemplate"

	"github.com/yargevad/filepathx"
)

type FuncMap = template.FuncMap

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
	if s.LayoutBody != "" {
		t, err = template.New(s.Name).Parse(s.LayoutBody)
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
	return t.Funcs(s.Funcs).ParseFiles(templates...)
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

type Manager[K GinContext] struct {
	re       multitemplate.Renderer
	funcs    FuncMap
	layouts  []string
	partials []string
	views    []*View
	mux      sync.Mutex
	base     string
}

func NewManager[K GinContext](re multitemplate.Renderer) *Manager[K] {
	return &Manager[K]{
		re:    re,
		funcs: FuncMap{},
		base:  "templates/",
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
	fooType := reflect.TypeOf(h)
	for i := 0; i < fooType.NumMethod(); i++ {
		method := fooType.Method(i)
		s.funcs[s.firstToLower(method.Name)] = func(args ...any) any {
			args = append([]any{h}, args...)
			inputs := make([]reflect.Value, len(args))
			for i := range args {
				inputs[i] = reflect.ValueOf(args[i])
			}
			return method.Func.Call(inputs)[0].Interface()
		}
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

func (s *Manager[K]) RenderViewByNameAndLayoutBody(name string, layout string) (string, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	var cv, mv *View
	for _, v := range s.views {
		if v.Name == name && v.LayoutBody == layout {
			cv = v
		}
		if v.Name == name && v.LayoutBody == "" {
			mv = v
		}
	}
	if cv != nil {
		return s.renderView(cv)
	}
	if mv != nil {
		cv = s.makeViewWithLayoutBody(mv, layout)
		s.views = append(s.views, cv)
		return s.renderView(cv)
	}
	return "", errors.New("view not found")
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
	var name string
	var rerr error
	c := ctx.GetGinContext()
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		if c.GetHeader("X-Layout") != "" {
			name, rerr = s.tm.RenderViewByNameAndLayoutBody(s.name, c.GetHeader("X-Layout"))
			if rerr != nil {
				panic(rerr)
			}
		}
		for name, vals := range c.Request.Header {
			if !strings.HasPrefix(name, "X-Update") {
				continue
			}
			tpl := s.tm.Build(s.name).WithLayoutBody(vals[0])
			str, rerr := tpl.ToString(ctx)
			if rerr != nil {
				panic(rerr)
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(str))
			c.Header(name, encoded)
		}

	} else {
		name, rerr = s.tm.RenderViewByNameAndLayout(s.name, s.layout)
		if rerr != nil {
			panic(rerr)
		}
	}
	c.HTML(code, name, ctx)
}

func (s *Template[K]) ToString(obj K) (res string, err error) {
	var b bytes.Buffer
	var v string
	if s.layoutBody == "" {
		v, err = s.tm.RenderViewByNameAndLayout(s.name, s.layout)
		if err != nil {
			return
		}
	} else {
		v, err = s.tm.RenderViewByNameAndLayoutBody(s.name, s.layoutBody)
		if err != nil {
			return
		}
	}
	//log.Infof("action template %v data: %+v", s.name, data)
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
