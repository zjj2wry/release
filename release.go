package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/google/go-github/github"
	flag "github.com/spf13/pflag"
	"golang.org/x/oauth2"
)

var (
	repository    string
	owner         string
	base          string
	last          int
	current       int
	token         string
	relnoteFilter bool
	releaseName   string
	tagName       string
	preRelease    bool
)

type byMerged []*github.PullRequest

func (a byMerged) Len() int           { return len(a) }
func (a byMerged) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byMerged) Less(i, j int) bool { return a[i].MergedAt.Before(*a[j].MergedAt) }

func init() {
	flag.StringVar(&repository, "repository", "console-web", "repository name")
	flag.StringVar(&owner, "owner", "caicloud", "repository owner")
	flag.IntVar(&last, "last", 0, "The PR number of the last versioned release.")
	flag.IntVar(&current, "current", 0, "The PR number of the current versioned release.")
	flag.StringVar(&token, "token", "", "Github api token for rate limiting. Background: https://developer.github.com/v3/#rate-limiting and create a token: https://github.com/settings/tokens")
	flag.StringVar(&base, "base", "master", "The base branch name for PRs to look for.")
	flag.BoolVar(&relnoteFilter, "relnote-filter", false, "Whether to filter PRs by the release-note label.")
	flag.StringVar(&releaseName, "releaseName", "", "release name")
	flag.StringVar(&tagName, "tagName", "", "release tag")
	flag.BoolVar(&preRelease, "preRelease", false, "Default LatestRelease")
}

func usage() {
	fmt.Printf(`usage: release --last=<number> --current=<number>
                     --token=<token> [--base=<branch-name>]
`)
}
func releaseUsage() {
	fmt.Printf(`usage: release --releaseName=<releaseName> --tagName=<tagName>
                     --preRelease=<false>
`)
}

func main() {
	flag.Parse()
	if last == 0 || token == "" {
		usage()
		os.Exit(1)
	}

	var tc *http.Client
	if len(token) > 0 {
		tc = oauth2.NewClient(
			oauth2.NoContext,
			oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: token}),
		)
	}
	client := github.NewClient(tc)

	opts := github.PullRequestListOptions{
		State:     "closed",
		Base:      base,
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			Page:    0,
			PerPage: 100,
		},
	}

	done := false
	prs := []*github.PullRequest{}
	var lastVersionMerged *time.Time
	var currentVersionMerged *time.Time
	ctx := context.Background()

	for !done {
		opts.Page++
		fmt.Printf("Fetching PR list page %2d\n", opts.Page)
		results, _, err := client.PullRequests.List(ctx, owner, repository, &opts)
		if err != nil {
			fmt.Printf("Error contacting github: %v", err)
			os.Exit(1)
		}
		unmerged := 0
		merged := 0
		if len(results) == 0 {
			done = true
			break
		}
		if current == 0 {
			current = *results[0].Number
			fmt.Println(current)
		}
		for ix := range results {
			result := results[ix]
			// Skip Closed but not Merged PRs
			if result.MergedAt == nil {
				unmerged++
				continue
			}

			if *result.Number == last {
				lastVersionMerged = result.MergedAt
				fmt.Printf(" ... found last PR %d.\n", last)
				break
			}
			if lastVersionMerged != nil && lastVersionMerged.After(*result.UpdatedAt) {
				done = true
				break
			}
			if *result.Number == current {
				currentVersionMerged = result.MergedAt
				fmt.Printf(" ... found current PR %d.\n", current)
			}
			prs = append(prs, result)
			merged++
		}
		fmt.Printf(" ... %d merged PRs, %d unmerged PRs.\n", merged, unmerged)
	}
	sort.Sort(byMerged(prs))
	buffer := &bytes.Buffer{}
	for _, pr := range prs {
		if lastVersionMerged.Before(*pr.MergedAt) && (pr.MergedAt.Before(*currentVersionMerged)) {
			if !relnoteFilter {
				fmt.Fprintf(buffer, "   * %s (#%d, @%s)\n", *pr.Title, *pr.Number, *pr.User.Login)
			} else {
				// Check to see if it has the release-note label.
				fmt.Printf(".")
				labels, _, err := client.Issues.ListLabelsByIssue(ctx, owner, repository, *pr.Number, &github.ListOptions{})
				// Sleep for 5 seconds to avoid irritating the API rate limiter.
				time.Sleep(5 * time.Second)
				if err != nil {
					fmt.Printf("Error contacting github: %v", err)
					os.Exit(1)
				}
				for _, label := range labels {
					if *label.Name == "release-note" {
						fmt.Fprintf(buffer, "   * %s (#%d, @%s)\n", *pr.Title, *pr.Number, *pr.User.Login)
					}
				}
			}
		}
	}
	fmt.Println()
	fmt.Printf("Release notes for PRs between #%d and #%d against branch %q:\n\n", last, current, base)
	fmt.Printf("%s", buffer.Bytes())

	//auto release
	if tagName == "" || releaseName == "" {
		releaseUsage()
		os.Exit(1)
	}
	body := buffer.String()
	release := &github.RepositoryRelease{
		TagName:    &tagName,
		Name:       &releaseName,
		Prerelease: &preRelease,
		Body:       &body,
	}
	repositoryRelease, _, err := client.Repositories.CreateRelease(ctx, "zjj2wry", "console-web", release)
	if err != nil {
		fmt.Printf("Error auto release: %v", err)
		os.Exit(1)
	}
	fmt.Println("realese url:", *repositoryRelease.HTMLURL)
}
