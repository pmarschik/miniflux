// Copyright 2017 Frédéric Guillot. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package storage // import "miniflux.app/storage"

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/lib/pq/hstore"
	"time"

	"miniflux.app/model"
	"miniflux.app/timer"
	"miniflux.app/timezone"
)

// FeedExists checks if the given feed exists.
func (s *Storage) FeedExists(userID, feedID int64) bool {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:FeedExists] userID=%d, feedID=%d", userID, feedID))

	var result int
	query := `SELECT count(*) as c FROM feeds WHERE user_id=$1 AND id=$2`
	s.db.QueryRow(query, userID, feedID).Scan(&result)
	return result >= 1
}

// FeedURLExists checks if feed URL already exists.
func (s *Storage) FeedURLExists(userID int64, feedURL string) bool {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:FeedURLExists] userID=%d, feedURL=%s", userID, feedURL))

	var result int
	query := `SELECT count(*) as c FROM feeds WHERE user_id=$1 AND feed_url=$2`
	s.db.QueryRow(query, userID, feedURL).Scan(&result)
	return result >= 1
}

// CountFeeds returns the number of feeds that belongs to the given user.
func (s *Storage) CountFeeds(userID int64) int {
	var result int
	err := s.db.QueryRow(`SELECT count(*) FROM feeds WHERE user_id=$1`, userID).Scan(&result)
	if err != nil {
		return 0
	}

	return result
}

// CountErrorFeeds returns the number of feeds with parse errors that belong to the given user.
func (s *Storage) CountErrorFeeds(userID int64) int {
	var result int
	err := s.db.QueryRow(`SELECT count(*) FROM feeds WHERE user_id=$1 AND parsing_error_count>=$2`, userID, maxParsingError).Scan(&result)
	if err != nil {
		return 0
	}

	return result
}

// Feeds returns all feeds of the given user.
func (s *Storage) Feeds(userID int64) (model.Feeds, error) {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:Feeds] userID=%d", userID))

	feeds := make(model.Feeds, 0)
	query := `SELECT
		f.id, f.feed_url, f.site_url, f.title, f.etag_header, f.last_modified_header,
		f.user_id, f.checked_at at time zone u.timezone,
		f.parsing_error_count, f.parsing_error_msg,
		f.scraper_rules, f.rewrite_rules, f.cookies, f.crawler,
		f.username, f.password,
		f.category_id, c.title as category_title,
		fi.icon_id,
		u.timezone
		FROM feeds f
		LEFT JOIN categories c ON c.id=f.category_id
		LEFT JOIN feed_icons fi ON fi.feed_id=f.id
		LEFT JOIN users u ON u.id=f.user_id
		WHERE f.user_id=$1
		ORDER BY f.parsing_error_count DESC, f.title ASC`

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("Unable to fetch feeds: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var feed model.Feed
		var iconID interface{}
		var tz string
		var cookies hstore.Hstore
		feed.Category = &model.Category{UserID: userID}

		err := rows.Scan(
			&feed.ID,
			&feed.FeedURL,
			&feed.SiteURL,
			&feed.Title,
			&feed.EtagHeader,
			&feed.LastModifiedHeader,
			&feed.UserID,
			&feed.CheckedAt,
			&feed.ParsingErrorCount,
			&feed.ParsingErrorMsg,
			&feed.ScraperRules,
			&feed.RewriteRules,
			&cookies,
			&feed.Crawler,
			&feed.Username,
			&feed.Password,
			&feed.Category.ID,
			&feed.Category.Title,
			&iconID,
			&tz,
		)

		if err != nil {
			return nil, fmt.Errorf("unable to fetch feeds row: %v", err)
		}

		if iconID != nil {
			feed.Icon = &model.FeedIcon{FeedID: feed.ID, IconID: iconID.(int64)}
		}

		for key, value := range cookies.Map {
			if value.Valid {
				feed.Cookies[key] = value.String
			}
		}

		feed.CheckedAt = timezone.Convert(tz, feed.CheckedAt)
		feeds = append(feeds, &feed)
	}

	return feeds, nil
}

// FeedByID returns a feed by the ID.
func (s *Storage) FeedByID(userID, feedID int64) (*model.Feed, error) {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:FeedByID] feedID=%d", feedID))

	var feed model.Feed
	var iconID interface{}
	var tz string
	var cookies hstore.Hstore
	feed.Category = &model.Category{UserID: userID}

	query := `
		SELECT
		f.id, f.feed_url, f.site_url, f.title, f.etag_header, f.last_modified_header,
		f.user_id, f.checked_at at time zone u.timezone,
		f.parsing_error_count, f.parsing_error_msg,
		f.scraper_rules, f.rewrite_rules, f.cookies, f.crawler,
		f.username, f.password,
		f.category_id, c.title as category_title,
		fi.icon_id,
		u.timezone
		FROM feeds f
		LEFT JOIN categories c ON c.id=f.category_id
		LEFT JOIN feed_icons fi ON fi.feed_id=f.id
		LEFT JOIN users u ON u.id=f.user_id
		WHERE f.user_id=$1 AND f.id=$2`

	err := s.db.QueryRow(query, userID, feedID).Scan(
		&feed.ID,
		&feed.FeedURL,
		&feed.SiteURL,
		&feed.Title,
		&feed.EtagHeader,
		&feed.LastModifiedHeader,
		&feed.UserID,
		&feed.CheckedAt,
		&feed.ParsingErrorCount,
		&feed.ParsingErrorMsg,
		&feed.ScraperRules,
		&feed.RewriteRules,
		&cookies,
		&feed.Crawler,
		&feed.Username,
		&feed.Password,
		&feed.Category.ID,
		&feed.Category.Title,
		&iconID,
		&tz,
	)

	switch {
	case err == sql.ErrNoRows:
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("unable to fetch feed: %v", err)
	}

	if iconID != nil {
		feed.Icon = &model.FeedIcon{FeedID: feed.ID, IconID: iconID.(int64)}
	}

	feed.Cookies = make(map[string]string, len(cookies.Map))
	for key, value := range cookies.Map {
		if value.Valid {
			feed.Cookies[key] = value.String
		}
	}

	feed.CheckedAt = timezone.Convert(tz, feed.CheckedAt)
	return &feed, nil
}

// CreateFeed creates a new feed.
func (s *Storage) CreateFeed(feed *model.Feed) error {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:CreateFeed] feedURL=%s", feed.FeedURL))
	sql := `
		INSERT INTO feeds
		(feed_url, site_url, title, category_id, user_id, etag_header, last_modified_header, crawler, username, password)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`

	err := s.db.QueryRow(
		sql,
		feed.FeedURL,
		feed.SiteURL,
		feed.Title,
		feed.Category.ID,
		feed.UserID,
		feed.EtagHeader,
		feed.LastModifiedHeader,
		feed.Crawler,
		feed.Username,
		feed.Password,
	).Scan(&feed.ID)
	if err != nil {
		return fmt.Errorf("unable to create feed: %v", err)
	}

	for i := 0; i < len(feed.Entries); i++ {
		feed.Entries[i].FeedID = feed.ID
		feed.Entries[i].UserID = feed.UserID
		err := s.createEntry(feed.Entries[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// UpdateFeed updates an existing feed.
func (s *Storage) UpdateFeed(feed *model.Feed) (err error) {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:UpdateFeed] feedURL=%s", feed.FeedURL))

	query := `UPDATE feeds SET
		feed_url=$1, site_url=$2, title=$3, category_id=$4, etag_header=$5, last_modified_header=$6, checked_at=$7,
		parsing_error_msg=$8, parsing_error_count=$9, scraper_rules=$10, rewrite_rules=$11, cookies=$12, crawler=$13,
		username=$14, password=$15
		WHERE id=$16 AND user_id=$17`

	cookies := hstore.Hstore{Map: make(map[string]sql.NullString)}

	if len(feed.Cookies) > 0 {
		for key, value := range feed.Cookies {
			cookies.Map[key] = sql.NullString{String: value, Valid: true}
		}
	}

	_, err = s.db.Exec(query,
		feed.FeedURL,
		feed.SiteURL,
		feed.Title,
		feed.Category.ID,
		feed.EtagHeader,
		feed.LastModifiedHeader,
		feed.CheckedAt,
		feed.ParsingErrorMsg,
		feed.ParsingErrorCount,
		feed.ScraperRules,
		feed.RewriteRules,
		cookies,
		feed.Crawler,
		feed.Username,
		feed.Password,
		feed.ID,
		feed.UserID,
	)

	if err != nil {
		return fmt.Errorf("Unable to update feed: %v", err)
	}

	return nil
}

// RemoveFeed removes a feed.
func (s *Storage) RemoveFeed(userID, feedID int64) error {
	defer timer.ExecutionTime(time.Now(), fmt.Sprintf("[Storage:RemoveFeed] userID=%d, feedID=%d", userID, feedID))

	result, err := s.db.Exec("DELETE FROM feeds WHERE id = $1 AND user_id = $2", feedID, userID)
	if err != nil {
		return fmt.Errorf("Unable to remove this feed: %v", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Unable to remove this feed: %v", err)
	}

	if count == 0 {
		return errors.New("no feed has been removed")
	}

	return nil
}

// ResetFeedErrors removes all feed errors.
func (s *Storage) ResetFeedErrors() error {
	_, err := s.db.Exec(`UPDATE feeds SET parsing_error_count=0, parsing_error_msg=''`)
	return err
}
