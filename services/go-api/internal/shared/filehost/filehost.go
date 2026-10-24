package filehost

import (
	"net/url"
	"regexp"
	"strings"
)

type host struct {
	provider   string
	hostnames  []string
	pathID     *regexp.Regexp
	validID    *regexp.Regexp
	page       func(id string) string
	download   func(id string) string
	fileOnPage func(id string) *regexp.Regexp
}

var pixeldrainID = regexp.MustCompile(`^[A-Za-z0-9]{6,16}$`)

var pillowsID = regexp.MustCompile(`^[A-Za-z0-9]{20,40}$`)

var imgurggID = regexp.MustCompile(`^[A-Za-z0-9]{5,16}$`)

var hosts = []host{
	{
		provider:  "pixeldrain",
		hostnames: []string{"pixeldrain.com"},
		pathID:    regexp.MustCompile(`^/u/([^/]+)$`),
		validID:   pixeldrainID,
		page:      func(id string) string { return "https://pixeldrain.com/u/" + id },
		download:  func(id string) string { return "https://pixeldrain.com/api/file/" + id + "?download" },
	},
	{
		provider:  "pillows",
		hostnames: []string{"pillows.su", "pillowcase.su"},
		pathID:    regexp.MustCompile(`^/f/([^/]+)$`),
		validID:   pillowsID,
		page:      func(id string) string { return "https://pillows.su/f/" + id },
		download:  func(id string) string { return "https://api.pillows.su/api/download/" + id },
	},
	{
		provider:  "imgurgg",
		hostnames: []string{"imgur.gg"},
		pathID:    regexp.MustCompile(`^/f/([^/]+)$`),
		validID:   imgurggID,
		page:      func(id string) string { return "https://imgur.gg/f/" + id },
		fileOnPage: func(id string) *regexp.Regexp {
			return regexp.MustCompile(`https://b\.imgur\.gg/` + id + `-[A-Za-z0-9._%()+-]{1,150}`)
		},
	},
}

type File struct {
	Provider string
	ID       string
	PageURL  string
}

func Parse(link string) (File, bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return File{}, false
	}
	name := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	for _, h := range hosts {
		if id, ok := h.idFrom(name, u.Path); ok {
			return File{Provider: h.provider, ID: id, PageURL: h.page(id)}, true
		}
	}
	return File{}, false
}

func (h host) idFrom(hostname, path string) (string, bool) {
	if !h.serves(hostname) {
		return "", false
	}
	m := h.pathID.FindStringSubmatch(path)
	if len(m) < 2 || !h.validID.MatchString(m[1]) {
		return "", false
	}
	return m[1], true
}

func (h host) serves(hostname string) bool {
	for _, name := range h.hostnames {
		if hostname == name {
			return true
		}
	}
	return false
}

func IsProvider(provider string) bool {
	_, ok := lookup(provider)
	return ok
}

func ValidID(provider, id string) bool {
	h, ok := lookup(provider)
	return ok && h.validID.MatchString(id)
}

func DownloadURL(provider, id string) (string, bool) {
	h, ok := lookup(provider)
	if !ok || h.download == nil || !h.validID.MatchString(id) {
		return "", false
	}
	return h.download(id), true
}

type PageFile struct {
	PageURL string
	File    *regexp.Regexp
}

func FileOnPage(provider, id string) (PageFile, bool) {
	h, ok := lookup(provider)
	if !ok || h.fileOnPage == nil || !h.validID.MatchString(id) {
		return PageFile{}, false
	}
	return PageFile{PageURL: h.page(id), File: h.fileOnPage(id)}, true
}

func lookup(provider string) (host, bool) {
	for _, h := range hosts {
		if h.provider == provider {
			return h, true
		}
	}
	return host{}, false
}
