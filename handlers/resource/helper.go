package resource

import (
	"fmt"
	"strings"

	h "github.com/dustin/go-humanize"
	ra "github.com/webtor-io/rest-api/services"
	w "github.com/webtor-io/web-ui/services/web"
)

type ButtonItem struct {
	ID         string
	CSRF       string
	ItemID     string
	ResourceID string
	Name       string
	Action     string
	Endpoint   string
}

type Breadcrumb struct {
	Name    string
	PathStr string
	Active  bool
}

type Pagination struct {
	Page   uint
	Active bool
	Prev   bool
	Next   bool
	Number bool
}

func (s *Helper) MakeButton(ctx *w.Context, gd *GetData, name string, action string, endpoint string) *ButtonItem {
	return &ButtonItem{
		ID:         gd.Item.ID,
		ItemID:     gd.Item.ID,
		ResourceID: gd.Resource.ID,
		Name:       name,
		Action:     action,
		Endpoint:   endpoint,
		CSRF:       ctx.CSRF,
	}
}

func (s *Helper) MakeDirButton(ctx *w.Context, gd *GetData, name string, action string, endpoint string) *ButtonItem {
	return &ButtonItem{
		ID:         gd.List.ID,
		ItemID:     gd.List.ID,
		ResourceID: gd.Resource.ID,
		Name:       name,
		Action:     action,
		Endpoint:   endpoint,
		CSRF:       ctx.CSRF,
	}
}

func (s *Helper) MakeResourceButton(ctx *w.Context, gd *GetData, name string, action string, endpoint string) *ButtonItem {
	return &ButtonItem{
		ID:         gd.Resource.ID,
		ItemID:     gd.Resource.ID,
		ResourceID: gd.Resource.ID,
		Name:       name,
		Action:     action,
		Endpoint:   endpoint,
		CSRF:       ctx.CSRF,
	}
}

func (s *Helper) MakeFileDownload(ctx *w.Context, gd *GetData) *ButtonItem {
	return s.MakeButton(ctx, gd,
		fmt.Sprintf("Download [%v]", h.Bytes(uint64(gd.Item.Size))),
		"download",
		"/download-file",
	)
}

func (s *Helper) MakeImage(ctx *w.Context, gd *GetData) *ButtonItem {
	return s.MakeButton(ctx, gd,
		"Preview",
		"preview",
		"/preview-image",
	)
}

func (s *Helper) MakeAudio(ctx *w.Context, gd *GetData) *ButtonItem {
	return s.MakeButton(ctx, gd,
		"Stream",
		"stream",
		"/stream-audio",
	)
}
func (s *Helper) MakeVideo(ctx *w.Context, gd *GetData) *ButtonItem {
	return s.MakeButton(ctx, gd,
		"Stream",
		"stream",
		"/stream-video",
	)
}

func (s *Helper) MakeDirDownload(ctx *w.Context, gd *GetData) *ButtonItem {
	return s.MakeDirButton(ctx, gd,
		fmt.Sprintf("Download Directory as ZIP [%v]", h.Bytes(uint64(gd.List.Size))),
		"download-dir",
		"/download-dir",
	)
}

func (s *Helper) MakeTorrentDownload(ctx *w.Context, gd *GetData) *ButtonItem {
	return s.MakeResourceButton(ctx, gd,
		fmt.Sprintf("Download Torrent File"),
		"download-torrent",
		"/download-torrent",
	)
}

func (s *Helper) HasBreadcrumbs(lr *ra.ListResponse) bool {
	hasDir := false
	for _, i := range lr.Items {
		if i.Type == ra.ListTypeDirectory {
			hasDir = true
			break
		}
	}
	return hasDir || lr.ListItem.PathStr != "/"
}

func (s *Helper) MakeBreadcrumbs(pathStr string) []Breadcrumb {
	var res []Breadcrumb
	if pathStr != "/" {
		var t []string
		path := strings.Split(strings.Trim(pathStr, "/"), "/")
		for _, p := range path {
			t = append(t, p)
			res = append(res, Breadcrumb{
				Name:    p,
				PathStr: "/" + strings.Join(t, "/"),
			})
		}
	}
	if len(res) > 0 {
		res[len(res)-1].Active = true
	}
	return res
}

func (s *Helper) HasPagination(lr *ra.ListResponse) bool {
	return lr.Count > len(lr.Items)
}

func (s *Helper) MakePagination(lr *ra.ListResponse, page uint, pageSize uint) []Pagination {
	var res []Pagination
	pages := uint(lr.Count)/pageSize + 1
	prev := page - 1
	if prev < 1 {
		prev = 1
	}
	next := page + 1
	if next > pages {
		next = pages
	}
	res = append(res, Pagination{
		Page:   prev,
		Active: prev != page,
		Prev:   true,
	})
	for i := uint(1); i < pages+1; i++ {
		res = append(res, Pagination{
			Page:   i,
			Active: i != page,
			Number: true,
		})
	}
	res = append(res, Pagination{
		Page:   next,
		Active: next != page,
		Next:   true,
	})
	return res
}

type Helper struct {
}

func NewHelper() *Helper {
	return &Helper{}
}
