// Copyright 2017 Frédéric Guillot. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package feed // import "miniflux.app/reader/feed"

import (
	"fmt"
	"time"

	"miniflux.app/errors"
	"miniflux.app/http/client"
	"miniflux.app/locale"
	"miniflux.app/logger"
	"miniflux.app/model"
	"miniflux.app/reader/icon"
	"miniflux.app/reader/processor"
	"miniflux.app/storage"
	"miniflux.app/timer"
)

var (
	errRequestFailed    = "Unable to execute request: %v"
	errServerFailure    = "Unable to fetch feed (Status Code = %d)"
	errDuplicate        = "This feed already exists (%s)"
	errNotFound         = "Feed %d not found"
	errEncoding         = "Unable to normalize encoding: %q"
	errCategoryNotFound = "Category not found for this user"
	errEmptyFeed        = "This feed is empty"
	errResourceNotFound = "Resource not found (404), this feed doesn't exists anymore, check the feed URL"
)

// Handler contains all the logic to create and refresh feeds.
type Handler struct {
	store      *storage.Storage
	translator *locale.Translator
}

// CreateFeed fetch, parse and store a new feed.
func (h *Handler) CreateFeed(userID, categoryID int64, url string, crawler bool, username, password string) (*model.Feed, error) {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Handler:CreateFeed] feedUrl=%s", url))

	if !h.store.CategoryExists(userID, categoryID) {
		return nil, errors.NewLocalizedError(errCategoryNotFound)
	}

	clt := client.New(url)
	clt.WithCredentials(username, password)
	response, err := clt.Get()
	if err != nil {
		if _, ok := err.(*errors.LocalizedError); ok {
			return nil, err
		}
		return nil, errors.NewLocalizedError(errRequestFailed, err)
	}

	if response.HasServerFailure() {
		return nil, errors.NewLocalizedError(errServerFailure, response.StatusCode)
	}

	// Content-Length = -1 when no Content-Length header is sent
	if response.ContentLength == 0 {
		return nil, errors.NewLocalizedError(errEmptyFeed)
	}

	if h.store.FeedURLExists(userID, response.EffectiveURL) {
		return nil, errors.NewLocalizedError(errDuplicate, response.EffectiveURL)
	}

	body, err := response.NormalizeBodyEncoding()
	if err != nil {
		return nil, errors.NewLocalizedError(errEncoding, err)
	}

	subscription, feedErr := parseFeed(body)
	if feedErr != nil {
		return nil, feedErr
	}

	feedProcessor := processor.NewFeedProcessor(userID, h.store, subscription)
	feedProcessor.WithCrawler(crawler)
	feedProcessor.Process()

	subscription.Category = &model.Category{ID: categoryID}
	subscription.EtagHeader = response.ETag
	subscription.LastModifiedHeader = response.LastModified
	subscription.FeedURL = response.EffectiveURL
	subscription.UserID = userID
	subscription.Crawler = crawler
	subscription.Username = username
	subscription.Password = password

	if subscription.SiteURL == "" {
		subscription.SiteURL = subscription.FeedURL
	}

	err = h.store.CreateFeed(subscription)
	if err != nil {
		return nil, err
	}

	logger.Debug("[Handler:CreateFeed] Feed saved with ID: %d", subscription.ID)

	icon, err := icon.FindIcon(subscription.SiteURL)
	if err != nil {
		logger.Error("[Handler:CreateFeed] %v", err)
	} else if icon == nil {
		logger.Info("No icon found for feedID=%d", subscription.ID)
	} else {
		h.store.CreateFeedIcon(subscription, icon)
	}

	return subscription, nil
}

// RefreshFeed fetch and update a feed if necessary.
func (h *Handler) RefreshFeed(userID, feedID int64) error {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Handler:RefreshFeed] feedID=%d", feedID))
	userLanguage, err := h.store.UserLanguage(userID)
	if err != nil {
		logger.Error("[Handler:RefreshFeed] %v", err)
		userLanguage = "en_US"
	}

	currentLanguage := h.translator.GetLanguage(userLanguage)

	originalFeed, err := h.store.FeedByID(userID, feedID)
	if err != nil {
		return err
	}

	if originalFeed == nil {
		return errors.NewLocalizedError(errNotFound, feedID)
	}

	clt := client.New(originalFeed.FeedURL)
	clt.WithCredentials(originalFeed.Username, originalFeed.Password)
	clt.WithCacheHeaders(originalFeed.EtagHeader, originalFeed.LastModifiedHeader)
	response, err := clt.Get()
	if err != nil {
		var customErr errors.LocalizedError
		if lerr, ok := err.(*errors.LocalizedError); ok {
			customErr = *lerr
		} else {
			customErr = *errors.NewLocalizedError(errRequestFailed, err)
		}

		originalFeed.ParsingErrorCount++
		originalFeed.ParsingErrorMsg = customErr.Localize(currentLanguage)
		h.store.UpdateFeed(originalFeed)
		return customErr
	}

	originalFeed.CheckedAt = time.Now()

	if response.IsNotFound() {
		err := errors.NewLocalizedError(errResourceNotFound)
		originalFeed.ParsingErrorCount++
		originalFeed.ParsingErrorMsg = err.Localize(currentLanguage)
		h.store.UpdateFeed(originalFeed)
		return err
	}

	if response.HasServerFailure() {
		err := errors.NewLocalizedError(errServerFailure, response.StatusCode)
		originalFeed.ParsingErrorCount++
		originalFeed.ParsingErrorMsg = err.Localize(currentLanguage)
		h.store.UpdateFeed(originalFeed)
		return err
	}

	if response.IsModified(originalFeed.EtagHeader, originalFeed.LastModifiedHeader) {
		logger.Debug("[Handler:RefreshFeed] Feed #%d has been modified", feedID)

		// Content-Length = -1 when no Content-Length header is sent
		if response.ContentLength == 0 {
			err := errors.NewLocalizedError(errEmptyFeed)
			originalFeed.ParsingErrorCount++
			originalFeed.ParsingErrorMsg = err.Localize(currentLanguage)
			h.store.UpdateFeed(originalFeed)
			return err
		}

		body, err := response.NormalizeBodyEncoding()
		if err != nil {
			return errors.NewLocalizedError(errEncoding, err)
		}

		subscription, parseErr := parseFeed(body)
		if parseErr != nil {
			originalFeed.ParsingErrorCount++
			originalFeed.ParsingErrorMsg = parseErr.Localize(currentLanguage)
			h.store.UpdateFeed(originalFeed)
			return err
		}

		feedProcessor := processor.NewFeedProcessor(userID, h.store, subscription)
		feedProcessor.WithScraperRules(originalFeed.ScraperRules)
		feedProcessor.WithRewriteRules(originalFeed.RewriteRules)
		feedProcessor.WithCookies(originalFeed.Cookies)
		feedProcessor.WithCrawler(originalFeed.Crawler)
		feedProcessor.Process()

		originalFeed.EtagHeader = response.ETag
		originalFeed.LastModifiedHeader = response.LastModified

		// Note: We don't update existing entries when the crawler is enabled (we crawl only inexisting entries).
		if err := h.store.UpdateEntries(originalFeed.UserID, originalFeed.ID, subscription.Entries, !originalFeed.Crawler); err != nil {
			return err
		}

		if !h.store.HasIcon(originalFeed.ID) {
			logger.Debug("[Handler:RefreshFeed] Looking for feed icon")
			icon, err := icon.FindIcon(originalFeed.SiteURL)
			if err != nil {
				logger.Debug("[Handler:RefreshFeed] %v", err)
			} else {
				h.store.CreateFeedIcon(originalFeed, icon)
			}
		}
	} else {
		logger.Debug("[Handler:RefreshFeed] Feed #%d not modified", feedID)
	}

	originalFeed.ParsingErrorCount = 0
	originalFeed.ParsingErrorMsg = ""

	if originalFeed.SiteURL == "" {
		originalFeed.SiteURL = originalFeed.FeedURL
	}

	return h.store.UpdateFeed(originalFeed)
}

// NewFeedHandler returns a feed handler.
func NewFeedHandler(store *storage.Storage, translator *locale.Translator) *Handler {
	return &Handler{store, translator}
}
