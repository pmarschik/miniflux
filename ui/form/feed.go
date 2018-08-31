// Copyright 2017 Frédéric Guillot. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package form // import "miniflux.app/ui/form"

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"miniflux.app/errors"
	"miniflux.app/model"
)

// FeedForm represents a feed form in the UI
type FeedForm struct {
	FeedURL      string
	SiteURL      string
	Title        string
	ScraperRules string
	RewriteRules string
	Cookies      map[string]string
	Crawler      bool
	CategoryID   int64
	Username     string
	Password     string
}

// ValidateModification validates FeedForm fields
func (f FeedForm) ValidateModification() error {
	if f.FeedURL == "" || f.SiteURL == "" || f.Title == "" || f.CategoryID == 0 {
		return errors.NewLocalizedError("All fields are mandatory.")
	}
	return nil
}

// Merge updates the fields of the given feed.
func (f FeedForm) Merge(feed *model.Feed) *model.Feed {
	feed.Category.ID = f.CategoryID
	feed.Title = f.Title
	feed.SiteURL = f.SiteURL
	feed.FeedURL = f.FeedURL
	feed.ScraperRules = f.ScraperRules
	feed.RewriteRules = f.RewriteRules
	feed.Cookies = f.Cookies
	feed.Crawler = f.Crawler
	feed.ParsingErrorCount = 0
	feed.ParsingErrorMsg = ""
	feed.Username = f.Username
	feed.Password = f.Password
	return feed
}

// NewFeedForm parses the HTTP request and returns a FeedForm
func NewFeedForm(r *http.Request) *FeedForm {
	categoryID, err := strconv.Atoi(r.FormValue("category_id"))
	if err != nil {
		categoryID = 0
	}

	cookies, err := parseCookies(r.FormValue("cookies"))
	if err != nil {
		cookies = make(map[string]string)
	}

	return &FeedForm{
		FeedURL:      r.FormValue("feed_url"),
		SiteURL:      r.FormValue("site_url"),
		Title:        r.FormValue("title"),
		ScraperRules: r.FormValue("scraper_rules"),
		RewriteRules: r.FormValue("rewrite_rules"),
		Cookies:      cookies,
		Crawler:      r.FormValue("crawler") == "1",
		CategoryID:   int64(categoryID),
		Username:     r.FormValue("feed_username"),
		Password:     r.FormValue("feed_password"),
	}
}

func parseCookies(rawCookies string) (map[string]string, error) {
	rawRequest := fmt.Sprintf("GET / HTTP/1.0\r\nCookie: %s\r\n\r\n", rawCookies)

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		return nil, err
	}

	cookies := make(map[string]string, len(req.Cookies()))
	for _, cookie := range req.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	return cookies, nil
}
