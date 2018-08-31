// Copyright 2017 Frédéric Guillot. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package scraper // import "miniflux.app/reader/scraper"

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"miniflux.app/http/client"
	"miniflux.app/logger"
	"miniflux.app/reader/readability"
	"miniflux.app/url"

	"github.com/PuerkitoBio/goquery"
)

// Fetch downloads a web page a returns relevant contents.
func Fetch(websiteURL, rules string, cookies map[string]string) (string, error) {
	clt := client.New(websiteURL).WithCookies(cookies)
	response, err := clt.Get()
	if err != nil {
		return "", err
	}

	if response.HasServerFailure() {
		return "", errors.New("scraper: unable to download web page")
	}

	if !strings.Contains(response.ContentType, "text/html") {
		return "", fmt.Errorf("scraper: this resource is not a HTML document (%s)", response.ContentType)
	}

	page, err := response.NormalizeBodyEncoding()
	if err != nil {
		return "", err
	}

	// The entry URL could redirect somewhere else.
	websiteURL = response.EffectiveURL

	if rules == "" {
		rules = getPredefinedScraperRules(websiteURL)
	}

	var content string
	if rules != "" {
		logger.Debug(`[Scraper] Using rules "%s" for "%s"`, rules, websiteURL)
		content, err = scrapContent(page, rules)
	} else {
		logger.Debug(`[Scraper] Using readability for "%s"`, websiteURL)
		content, err = readability.ExtractContent(page)
	}

	if err != nil {
		return "", err
	}

	return content, nil
}

func scrapContent(page io.Reader, rules string) (string, error) {
	document, err := goquery.NewDocumentFromReader(page)
	if err != nil {
		return "", err
	}

	contents := ""
	document.Find(rules).Each(func(i int, s *goquery.Selection) {
		var content string

		// For some inline elements, we get the parent.
		if s.Is("img") || s.Is("iframe") {
			content, _ = s.Parent().Html()
		} else {
			content, _ = s.Html()
		}

		contents += content
	})

	return contents, nil
}

func getPredefinedScraperRules(websiteURL string) string {
	urlDomain := url.Domain(websiteURL)

	for domain, rules := range predefinedRules {
		if strings.Contains(urlDomain, domain) {
			return rules
		}
	}

	return ""
}
