// Copyright 2013 The uc-cdis AUTHORS. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mygithub

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/go-github/github"
)

const (
	libraryVersion                    = "14"
	defaultBaseURL                    = "https://api.github.com/"
	uploadBaseURL                     = "https://uploads.github.com/"
	userAgent                         = "go-github/" + libraryVersion
	mediaTypeProtectedBranchesPreview = "application/vnd.github.loki-preview+json"
)

// A MyClient manages communication with the GitHub API.
type MyClient struct {
	client       *github.Client // HTTP client used to communicate with the API.
	common       myservice      // Reuse a single struct instead of allocating one for each service on the heap.
	Repositories *MyRepositoriesService
}

type myservice struct {
	client *MyClient
}
type rateLimitCategory uint8

const (
	coreCategory rateLimitCategory = iota
	searchCategory

	categories // An array of this length will be able to contain all rate limit categories.
)

// NewClient returns a new GitHub API client. If a nil httpClient is
// provided, http.DefaultClient will be used. To use API methods which require
// authentication, provide an http.Client that will perform the authentication
// for you (such as that provided by the golang.org/x/oauth2 library).
func NewClient(client *github.Client) *MyClient {
	c := &MyClient{client: client}
	c.common.client = c
	c.Repositories = (*MyRepositoriesService)(&c.common)
	return c
}

// MyRepositoriesService handles communication with the repository related
// methods of the GitHub API.
// It is a customized type of customized RepositoriesService
//
// GitHub API docs: https://developer.github.com/v3/repos/
type MyRepositoriesService myservice

// CdisProtectionRequest represents a request to create/edit a branch's protection.
type CdisProtectionRequest struct {
	RequiredStatusChecks       *github.RequiredStatusChecks          `json:"required_status_checks"`
	RequiredPullRequestReviews *PullRequestReviewsEnforcementRequest `json:"required_pull_request_reviews"`
	EnforceAdmins              bool                                  `json:"enforce_admins"`
	Restrictions               *github.BranchRestrictionsRequest     `json:"restrictions"`
}

// PullRequestReviewsEnforcementRequest represents request to set the pull request review
// enforcement of a protected branch. It is separate from PullRequestReviewsEnforcement above
// because the request structure is different from the response structure.
type PullRequestReviewsEnforcementRequest struct {
	// Specifies which users and teams should be allowed to dismiss pull request reviews. Can be nil to disable the restrictions.
	DismissalRestrictionsRequest *github.DismissalRestrictionsRequest `json:"dismissal_restrictions"`
	// Specifies if approved reviews can be dismissed automatically, when a new commit is pushed. (Required)
	DismissStaleReviews bool `json:"dismiss_stale_reviews"`
	// RequireCodeOwnerReviews specifies if an approved review is required in pull requests including files with a designated code owner.
	RequireCodeOwnerReviews bool `json:"require_code_owner_reviews"`
}

// MarshalJSON implements the json.Marshaler interface.
// Converts nil value of PullRequestReviewsEnforcementRequest.DismissalRestrictionsRequest to empty array
func (req PullRequestReviewsEnforcementRequest) MarshalJSON() ([]byte, error) {
	if req.DismissalRestrictionsRequest == nil {
		newReq := struct {
			D bool `json:"dismiss_stale_reviews"`
			O bool `json:"require_code_owner_reviews"`
		}{
			D: req.DismissStaleReviews,
			O: req.RequireCodeOwnerReviews,
		}
		return json.Marshal(newReq)
	}
	newReq := struct {
		R *github.DismissalRestrictionsRequest `json:"dismissal_restrictions"`
		D bool                                 `json:"dismiss_stale_reviews"`
		O bool                                 `json:"require_code_owner_reviews"`
	}{
		R: req.DismissalRestrictionsRequest,
		D: req.DismissStaleReviews,
		O: req.RequireCodeOwnerReviews,
	}
	return json.Marshal(newReq)
}

// UpdateBranchProtection updates the protection of a given branch.
//
// GitHub API docs: https://developer.github.com/v3/repos/branches/#update-branch-protection
func (s *MyRepositoriesService) UpdateBranchProtection(ctx context.Context, owner, repo, branch string, preq *CdisProtectionRequest) (*github.Protection, *github.Response, error) {
	u := fmt.Sprintf("repos/%v/%v/branches/%v/protection", owner, repo, branch)
	req, err := s.client.client.NewRequest("PUT", u, preq)
	if err != nil {
		return nil, nil, err
	}

	// TODO: remove custom Accept header when this API fully launches
	req.Header.Set("Accept", mediaTypeProtectedBranchesPreview)

	p := new(github.Protection)
	resp, err := s.client.client.Do(ctx, req, p)
	if err != nil {
		return nil, resp, err
	}

	return p, resp, nil
}
