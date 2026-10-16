package email

import (
	"bytes"
	"html/template"
	"strconv"
	"strings"
)

// DefaultHeaderColor is Talyvor's Obsidian, the header of a workspace that set no colour.
const DefaultHeaderColor = "#060A12"

// Brand is how one workspace's mail looks (B39.18). Name is empty for an unbranded workspace, which keeps the
// configured From display name.
type Brand struct {
	Name, Color, LogoURL string
}

// brandFor reads a workspace's name, logo_url and email_header_color: a workspace that set a logo or a colour is
// branded with its name, and anything it left unset falls back to Talyvor's default.
func (s *Sink) brandFor(name, logoURL, color string) Brand {
	if !strings.HasPrefix(logoURL, "https://") {
		logoURL = "" // a logo stored before B39.18 refused anything but https
	}
	if logoURL == "" && color == "" {
		b := Brand{Color: DefaultHeaderColor}
		if s.baseURL != "" {
			b.LogoURL = s.baseURL + "/email-logo.png" // the front end serves frontend/public at its root
		}
		return b
	}
	if color == "" {
		color = DefaultHeaderColor
	}
	return Brand{Name: name, Color: color, LogoURL: logoURL}
}

var layout = template.Must(template.New("mail").Parse(`<!doctype html>
<html><body style="margin:0;padding:0;background:#F4F7FB">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#F4F7FB"><tr><td align="center" style="padding:24px 12px">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:600px;background:#FFFFFF;border-radius:6px;overflow:hidden">
<tr><td style="background:{{.Color}};padding:20px 24px">{{if .LogoURL}}<img src="{{.LogoURL}}" alt="{{.Alt}}" height="32" style="display:block;height:32px;max-width:100%;border:0">{{else}}<span style="font-family:'Space Grotesk',Arial,sans-serif;font-size:20px;font-weight:600;color:{{.Ink}}">{{.Name}}</span>{{end}}</td></tr>
<tr><td style="padding:24px;font-family:'Space Grotesk',Arial,sans-serif;font-size:15px;line-height:1.5;color:#060A12;white-space:pre-wrap">{{.Body}}</td></tr>
</table></td></tr></table></body></html>`))

// render is the HTML part of a mail: the brand's colour and logo (or name) in the header, the text body below.
func render(b Brand, body string) string {
	alt := b.Name
	if alt == "" {
		alt = "Talyvor"
	}
	var out bytes.Buffer
	_ = layout.Execute(&out, map[string]string{
		"Color": b.Color, "LogoURL": b.LogoURL, "Name": b.Name, "Alt": alt, "Ink": ink(b.Color), "Body": body,
	})
	return out.String()
}

// ink is the header text colour that reads on bg: Obsidian on a light colour, white on a dark one.
func ink(bg string) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(bg, "#"), 16, 32)
	if err != nil {
		return "#FFFFFF"
	}
	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	if 0.299*r+0.587*g+0.114*b > 150 {
		return "#060A12"
	}
	return "#FFFFFF"
}
