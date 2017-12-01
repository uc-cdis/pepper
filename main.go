/*
Copyright 2016 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/oauth2"

	"github.com/Sirupsen/logrus"
	"github.com/google/go-github/github"
	"github.com/pepper/mygithub"
)

const (
	// BANNER is what is printed for help/info output.
	BANNER = "pepper - %s\n"
	// VERSION is the binary version.
	VERSION = "v0.1.0"
)

var (
	token  string
	enturl string
	org    string
	nouser bool
	dryrun bool

	debug      bool
	version    bool
	exceptions map[string]stringSlice
)

// stringSlice is a slice of strings
type stringSlice []string

// implement the flag interface for stringSlice
func (s *stringSlice) String() string {
	return fmt.Sprintf("%s", *s)
}
func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func init() {
	// parse flags
	flag.StringVar(&token, "token", "", "GitHub API token")
	flag.StringVar(&enturl, "url", "", "GitHub Enterprise URL")
	flag.StringVar(&org, "org", "", "organization to include")
	flag.BoolVar(&nouser, "nouser", false, "do not include your user")
	flag.BoolVar(&dryrun, "dry-run", false, "do not change branch settings just print the changes that would occur")

	flag.BoolVar(&version, "version", false, "print version and exit")
	flag.BoolVar(&version, "v", false, "print version and exit (shorthand)")
	flag.BoolVar(&debug, "d", false, "run in debug mode")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, fmt.Sprintf(BANNER, VERSION))
		flag.PrintDefaults()
	}

	flag.Parse()

	if version {
		fmt.Printf("%s", VERSION)
		os.Exit(0)
	}

	// set log level
	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if token == "" {
		usageAndExit("GitHub token cannot be empty.", 1)
	}

	if nouser && org == "" {
		usageAndExit("no organizations provided", 1)
	}
	file, e := ioutil.ReadFile("./exception-repos.json")
	if e != nil {
		fmt.Printf("File error: %v\n", e)
		os.Exit(1)
	}
	e = json.Unmarshal(file, &exceptions)
	if e != nil {
		fmt.Printf("Json error: %v\n", e)
		os.Exit(1)
	}
}

type fn func(context.Context, *github.Client, *mygithub.MyClient, string, int, int) (int, error)

func main() {
	// On ^C, or SIGTERM handle exit.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		for sig := range c {
			logrus.Infof("Received %s, exiting.", sig.String())
			os.Exit(0)
		}
	}()

	// Create the http client.
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	// Create the github client.
	client := github.NewClient(tc)
	if enturl != "" {
		var err error
		client.BaseURL, err = url.Parse(enturl + "/api/v3/")
		if err != nil {
			logrus.Fatal(err)
		}
	}

	if !nouser {
		// Get the current user
		user, _, err := client.Users.Get(context.Background(), "")
		if err != nil {
			logrus.Fatal(err)
		}
		updateRepositories(client, *user.Login, getRepositories)
		// add the current user to orgs
	} else {
		updateRepositories(client, org, getRepositoriesByOrg)
	}
}

func updateRepositories(client *github.Client, subjectName string, function fn) {
	page := 1
	perPage := 20
	myClient := mygithub.NewClient(client)
	curPage, err := function(context.Background(), client, myClient, subjectName, page, perPage)
	for curPage != -1 && err == nil {
		curPage, err = function(context.Background(), client, myClient, subjectName, curPage, perPage)
	}
	if err != nil {
		logrus.Fatal(err)
	}

}

func getRepositories(ctx context.Context, client *github.Client, myClient *mygithub.MyClient, user string, page int, perPage int) (int, error) {
	opt := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{
			Page:    page,
			PerPage: perPage,
		},
	}
	repos, resp, err := client.Repositories.List(ctx, user, opt)
	if err != nil {
		return -1, err
	}
	return handleRepoAndNext(ctx, client, myClient, user, repos, page, resp)
}

func getRepositoriesByOrg(ctx context.Context, client *github.Client, myClient *mygithub.MyClient, org string, page int, perPage int) (int, error) {
	fmt.Println("By org")
	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{
			Page:    page,
			PerPage: perPage,
		},
	}
	repos, resp, err := client.Repositories.ListByOrg(ctx, org, opt)
	if err != nil {
		return -1, err
	}
	return handleRepoAndNext(ctx, client, myClient, org, repos, page, resp)
}

func handleRepoAndNext(ctx context.Context, client *github.Client, myClient *mygithub.MyClient, subject string, repos []*github.Repository, page int, resp *github.Response) (int, error) {
	for _, repo := range repos {
		if subject != *repo.Owner.Login || in(exceptions["exceptions"], *repo.FullName) {
			continue
		}
		if err := handleRepo(ctx, client, myClient, repo); err != nil {
			logrus.Warn(err)
		}
	}

	// Return early if we are on the last page.
	if page == resp.LastPage || resp.NextPage == 0 {
		return -1, nil
	}

	return resp.NextPage, nil
}

// handleRepo will return nil error if the user does not have access to something.
func handleRepo(ctx context.Context, client *github.Client, myClient *mygithub.MyClient, repo *github.Repository) error {
	fmt.Println(*repo.FullName)
	fmt.Println(*repo.DefaultBranch)
	branch, resp, err := client.Repositories.GetBranch(ctx, *repo.Owner.Login, *repo.Name, *repo.DefaultBranch)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil
	}
	if err != nil {
		return err
	}
	return handleBranch(ctx, client, myClient, repo, branch)
}

func handleBranch(ctx context.Context, client *github.Client, myClient *mygithub.MyClient, repo *github.Repository, branch *github.Branch) error {
	protectionRequest := &mygithub.CdisProtectionRequest{
		RequiredStatusChecks: nil,
		// &github.RequiredStatusChecks{
		// 	Strict:   true,
		// 	Contexts: []string{"continuous-integration/travis-ci", "codacy/pr"},
		// },
		RequiredPullRequestReviews: &mygithub.PullRequestReviewsEnforcementRequest{
			DismissStaleReviews:     false,
			RequireCodeOwnerReviews: true,
		},
		EnforceAdmins: true,
		// TODO: Only organization repositories can have users and team restrictions.
		//       In order to be able to test these Restrictions, need to add support
		//       for creating temporary organization repositories.
		Restrictions: nil,
	}

	if branch.Protected != nil && *branch.Protected {
		fmt.Printf("[OK] %s:%s is already protected\n", *repo.FullName, *branch.Name)
		return nil
	}

	fmt.Printf("[UPDATE] %s:%s will be changed to protected\n", *repo.FullName, *branch.Name)
	if dryrun {
		// return early
		return nil
	}

	// set the branch to be protected
	b := true
	branch.Protected = &b
	if _, _, err := myClient.Repositories.UpdateBranchProtection(ctx, *repo.Owner.Login, *repo.Name, *branch.Name, protectionRequest); err != nil {
		return err
	}
	fmt.Printf("[UPDATE] %s:%s has been changed to protected\n", *repo.FullName, *branch.Name)
	return nil
}

func in(a stringSlice, s string) bool {
	for _, b := range a {
		if b == s {
			return true
		}
	}
	return false
}

func usageAndExit(message string, exitCode int) {
	if message != "" {
		fmt.Fprintf(os.Stderr, message)
		fmt.Fprintf(os.Stderr, "\n\n")
	}
	flag.Usage()
	fmt.Fprintf(os.Stderr, "\n")
	os.Exit(exitCode)
}
