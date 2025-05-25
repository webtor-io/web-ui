package parsetorrentname

import (
	"reflect"
	"strconv"
	"strings"
)

// TorrentInfo is the resulting structure returned by Parse
type TorrentInfo struct {
	Title       string `json:"title,omitempty"`
	ExtraTitle  string `json:"extra_title,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Year        int    `json:"year,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Bitrate     string `json:"bitrate,omitempty"`
	Quality     string `json:"quality,omitempty"`
	ColorDepth  string `json:"color_depth,omitempty"`
	Codec       string `json:"codec,omitempty"`
	Audio       string `json:"audio,omitempty"`
	Group       string `json:"group,omitempty"`
	Studio      string `json:"studio,omitempty"`
	Region      string `json:"region,omitempty"`
	Extended    bool   `json:"extended,omitempty"`
	Hardcoded   bool   `json:"hardcoded,omitempty"`
	Proper      bool   `json:"proper,omitempty"`
	Repack      bool   `json:"repack,omitempty"`
	Container   string `json:"container,omitempty"`
	Widescreen  bool   `json:"widescreen,omitempty"`
	Website     string `json:"website,omitempty"`
	Language    string `json:"language,omitempty"`
	Sbs         string `json:"sbs,omitempty"`
	Unrated     bool   `json:"unrated,omitempty"`
	Size        string `json:"size,omitempty"`
	Threed      bool   `json:"3d,omitempty"`
	Porn        bool   `json:"porn,omitempty"`
	Avc         bool   `json:"avc,omitempty"`
	SplitScenes bool   `json:"split_scenes,omitempty"`
	Scene       int    `json:"scene,omitempty"`
	Dubbing     string `json:"dubbing,omitempty"`
}

func (s *TorrentInfo) mapField(tor *TorrentInfo, fieldType FieldType, val string) {
	ttor := reflect.TypeOf(tor)
	torV := reflect.ValueOf(tor)
	field := strings.Replace(strings.Title(strings.Replace(string(fieldType), "_", " ", -1)), " ", "", -1)
	v, ok := ttor.Elem().FieldByName(field)
	if !ok {
		return
	}
	//fmt.Printf("    field=%v, type=%+v, value=%v\n", field, v.Type, val)
	switch v.Type.Kind() {
	case reflect.Bool:
		torV.Elem().FieldByName(field).SetBool(true)
	case reflect.Int:
		clean, _ := strconv.ParseInt(val, 10, 64)
		torV.Elem().FieldByName(field).SetInt(clean)
	case reflect.Uint:
		clean, _ := strconv.ParseUint(val, 10, 64)
		torV.Elem().FieldByName(field).SetUint(clean)
	case reflect.String:
		torV.Elem().FieldByName(field).SetString(val)
	}
}
