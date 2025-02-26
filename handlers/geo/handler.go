package geo

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/geoip"
	"net"
	"net/http"
	"strings"
)

func getIp(c *gin.Context) net.IP {
	var ipStr string
	if xff := c.Request.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ipStr = strings.TrimSpace(parts[0])
	} else if xrip := c.Request.Header.Get("X-Real-IP"); xrip != "" {
		ipStr = xrip
	} else {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			host = c.Request.RemoteAddr // fallback in case of error
		}
		ipStr = host
	}
	return net.ParseIP(ipStr)
}

func RegisterHandler(api *geoip.Api, r *gin.Engine) error {
	if api == nil {
		return nil
	}
	r.Use(func(c *gin.Context) {
		var ip net.IP
		if coo, err := c.Cookie("test-ip"); err == nil {
			ip = net.ParseIP(coo)
		} else if c.Query("test-ip") != "" {
			ip = net.ParseIP(c.Query("test-ip"))
		} else {
			ip = getIp(c)
		}
		if ip == nil {
			return
		}
		if ip.To4() == nil {
			return
		}
		data, err := api.Get(ip)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to fetch geoip data"))
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), geoip.Data{}, data))
	})
	return nil
}

func GetFromContext(c *gin.Context) *geoip.Data {
	if gd := c.Request.Context().Value(geoip.Data{}); gd != nil {
		return gd.(*geoip.Data)
	}
	return nil
}
