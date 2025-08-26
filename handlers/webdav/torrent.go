package webdav

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/webdav"
)

type TorrentDirectory struct {
	*BaseDirectory
	api *api.Api
}

func (s *TorrentDirectory) Stat(ctx context.Context, tr *models.TorrentResource, path string) (*webdav.FileInfo, error) {
	li, err := s.retrieveTorrentItemWithoutPrefix(ctx, tr.ResourceID, path)
	if err != nil {
		return nil, err
	}
	if li == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	fi := s.listItemToFileInfo(li, tr)
	return &fi, nil
}

func (s *TorrentDirectory) ReadDir(ctx context.Context, tr *models.TorrentResource, path string, recursive bool) ([]webdav.FileInfo, error) {
	items, err := s.retrieveTorrentItemsWithoutPrefix(ctx, tr.ResourceID, path)
	if err != nil {
		return nil, err
	}

	fis := make([]webdav.FileInfo, len(items))
	for n, i := range items {
		fis[n] = s.listItemToFileInfo(&i, tr)
	}

	return fis, nil
}

func (s *TorrentDirectory) Open(ctx context.Context, tr *models.TorrentResource, path string) (io.ReadCloser, *url.URL, error) {
	ti, err := s.retrieveTorrentItemWithoutPrefix(ctx, tr.ResourceID, path)
	if err != nil {
		return nil, nil, err
	}
	if ti == nil {
		return nil, nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	cu, err := s.getContentURL(ctx, tr.ResourceID, ti.ID)
	if err != nil {
		return nil, nil, err
	}
	return nil, cu, nil

}

func (s *TorrentDirectory) listItemToFileInfo(i *ra.ListItem, tr *models.TorrentResource) webdav.FileInfo {
	if i.Type == ra.ListTypeDirectory {
		return webdav.FileInfo{
			Path:    i.PathStr + "/",
			ModTime: tr.CreatedAt,
			IsDir:   true,
			Size:    i.Size,
		}
	} else {
		return webdav.FileInfo{
			Path:     i.PathStr,
			ModTime:  tr.CreatedAt,
			IsDir:    false,
			Size:     i.Size,
			MIMEType: i.MimeType,
		}
	}
}

func (s *TorrentDirectory) getContentURL(ctx context.Context, rID, tID string) (*url.URL, error) {
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	er, err := s.api.ExportResourceContent(ctx, wcc.ApiClaims, rID, tID, "")
	if err != nil {
		return nil, err
	}
	return url.Parse(er.ExportItems["download"].URL)
}

func (s *TorrentDirectory) retrieveTorrentItemsWithoutPrefix(ctx context.Context, hash string, path string) ([]ra.ListItem, error) {
	path = strings.TrimSuffix(path, "/")
	prefix, err := s.getPrefix(ctx, hash)
	if err != nil {
		return nil, err
	}
	ls, err := s.retrieveTorrentItems(ctx, hash, prefix+path)
	for i, l := range ls {
		ls[i].PathStr = strings.TrimPrefix(l.PathStr, prefix)
	}
	return ls, err
}

func (s *TorrentDirectory) retrieveTorrentItemWithoutPrefix(ctx context.Context, hash string, path string) (*ra.ListItem, error) {
	prefix, err := s.getPrefix(ctx, hash)
	if err != nil {
		return nil, err
	}
	l, err := s.retrieveTorrentItem(ctx, hash, prefix+path)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, nil
	}
	l.PathStr = strings.TrimPrefix(l.PathStr, prefix)
	return l, err
}

func (s *TorrentDirectory) retrieveTorrentItems(ctx context.Context, hash string, path string) ([]ra.ListItem, error) {
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := uint(100)
	offset := uint(0)
	var items []ra.ListItem
	for {
		resp, err := s.api.ListResourceContentCached(ctx, wcc.ApiClaims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
			Output: api.OutputTree,
			Path:   path,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, resp.Items...)
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return items, nil
}

func (s *TorrentDirectory) retrieveTorrentItem(ctx context.Context, hash string, path string) (*ra.ListItem, error) {
	path = strings.TrimSuffix(path, "/")
	wcc, err := getWebContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := uint(100)
	offset := uint(0)
	for {
		resp, err := s.api.ListResourceContentCached(ctx, wcc.ApiClaims, hash, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			if item.PathStr == path {
				return &item, nil
			}
		}
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return nil, nil
}

func (s *TorrentDirectory) getPrefix(ctx context.Context, hash string) (string, error) {
	prefix := ""
	items, err := s.retrieveTorrentItems(ctx, hash, "/")
	if err != nil {
		return "", err
	}
	if len(items) == 1 && items[0].Type == ra.ListTypeDirectory {
		prefix = items[0].PathStr
	}
	return prefix, nil
}
