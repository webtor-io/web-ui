package s3

import (
	"encoding/xml"
	"time"
)

const s3NS = "http://s3.amazonaws.com/doc/2006-03-01/"

// iso8601Millis is the timestamp format S3 uses in listings. Object
// LastModified in a listing is this format; the HTTP Last-Modified header on
// GET/HEAD stays RFC 1123 as usual.
const iso8601Millis = "2006-01-02T15:04:05.000Z"

type bucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

type owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName,omitempty"`
}

type listAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	XMLNS   string   `xml:"xmlns,attr"`
	Owner   owner    `xml:"Owner"`
	Buckets struct {
		Bucket []bucket `xml:"Bucket"`
	} `xml:"Buckets"`
}

type object struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
	Owner        *owner `xml:"Owner,omitempty"`
}

type commonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// listBucketResult covers both ListObjects (v1) and ListObjectsV2. The two
// differ only in the paging fields, and encoding/xml drops the empty ones, so a
// single struct with omitempty keeps one code path for both — v1 is still what
// several desktop clients (and rclone with list-version 1) send.
type listBucketResult struct {
	XMLName xml.Name `xml:"ListBucketResult"`
	XMLNS   string   `xml:"xmlns,attr"`

	Name        string `xml:"Name"`
	Prefix      string `xml:"Prefix"`
	Delimiter   string `xml:"Delimiter,omitempty"`
	MaxKeys     int    `xml:"MaxKeys"`
	IsTruncated bool   `xml:"IsTruncated"`

	// v1 paging
	Marker     string `xml:"Marker,omitempty"`
	NextMarker string `xml:"NextMarker,omitempty"`

	// v2 paging. KeyCount is a pointer so that an empty v2 listing can say
	// KeyCount=0 explicitly while a v1 response omits the element entirely —
	// with a plain int, one of the two is always wrong.
	KeyCount              *int   `xml:"KeyCount,omitempty"`
	ContinuationToken     string `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string `xml:"NextContinuationToken,omitempty"`
	StartAfter            string `xml:"StartAfter,omitempty"`

	EncodingType string `xml:"EncodingType,omitempty"`

	Contents       []object       `xml:"Contents"`
	CommonPrefixes []commonPrefix `xml:"CommonPrefixes"`
}

type locationConstraint struct {
	XMLName xml.Name `xml:"LocationConstraint"`
	XMLNS   string   `xml:"xmlns,attr"`
	Value   string   `xml:",chardata"`
}

type copyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	XMLNS        string   `xml:"xmlns,attr"`
	LastModified string   `xml:"LastModified"`
	ETag         string   `xml:"ETag"`
}

func formatListTime(t time.Time) string {
	if t.IsZero() {
		return time.Unix(0, 0).UTC().Format(iso8601Millis)
	}
	return t.UTC().Format(iso8601Millis)
}
