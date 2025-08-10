package webdav

import "encoding/xml"

// WebDAV XML structures for protocol compliance

// Multistatus represents the WebDAV multistatus response
type Multistatus struct {
	XMLName   xml.Name   `xml:"DAV: multistatus"`
	Responses []Response `xml:"response"`
}

// Response represents a single resource response in multistatus
type Response struct {
	XMLName xml.Name `xml:"DAV: response"`
	Href    string   `xml:"href"`
	Propstat Propstat `xml:"propstat"`
}

// Propstat contains property status information
type Propstat struct {
	XMLName xml.Name `xml:"DAV: propstat"`
	Prop    Prop     `xml:"prop"`
	Status  string   `xml:"status"`
}

// Prop contains WebDAV properties
type Prop struct {
	XMLName           xml.Name          `xml:"DAV: prop"`
	DisplayName       *string           `xml:"displayname,omitempty"`
	ResourceType      *ResourceType     `xml:"resourcetype,omitempty"`
	ContentLength     *int64            `xml:"getcontentlength,omitempty"`
	ContentType       *string           `xml:"getcontenttype,omitempty"`
	LastModified      *string           `xml:"getlastmodified,omitempty"`
	CreationDate      *string           `xml:"creationdate,omitempty"`
	SupportedLock     *SupportedLock    `xml:"supportedlock,omitempty"`
	LockDiscovery     *LockDiscovery    `xml:"lockdiscovery,omitempty"`
}

// ResourceType indicates if the resource is a collection (directory) or not
type ResourceType struct {
	XMLName    xml.Name    `xml:"DAV: resourcetype"`
	Collection *Collection `xml:"collection,omitempty"`
}

// Collection represents a WebDAV collection (directory)
type Collection struct {
	XMLName xml.Name `xml:"DAV: collection"`
}

// SupportedLock represents supported locking mechanisms
type SupportedLock struct {
	XMLName xml.Name `xml:"DAV: supportedlock"`
}

// LockDiscovery represents active locks
type LockDiscovery struct {
	XMLName xml.Name `xml:"DAV: lockdiscovery"`
}

// PropFind represents a PROPFIND request
type PropFind struct {
	XMLName xml.Name `xml:"DAV: propfind"`
	Prop    *Prop    `xml:"prop,omitempty"`
	AllProp *AllProp `xml:"allprop,omitempty"`
	PropName *PropName `xml:"propname,omitempty"`
}

// AllProp requests all properties
type AllProp struct {
	XMLName xml.Name `xml:"DAV: allprop"`
}

// PropName requests property names only
type PropName struct {
	XMLName xml.Name `xml:"DAV: propname"`
}

// WebDAV folder types
type FolderType string

const (
	FolderTypeTorrents FolderType = "torrents"
	FolderTypeAll      FolderType = "all"
	FolderTypeMovies   FolderType = "movies"
	FolderTypeSeries   FolderType = "series"
)

// FileInfo represents file information for WebDAV responses
type FileInfo struct {
	Name         string
	Size         int64
	ModTime      string
	IsDir        bool
	ContentType  string
	ResourceID   string
	Path         string
}
