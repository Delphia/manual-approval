package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v61/github"
)

type approvalEnvironment struct {
	client              *github.Client
	repoFullName        string
	repo                string
	repoOwner           string
	runID               int
	approvalIssue       *github.Issue
	approvalIssueNumber int
	issueTitle          string
	issueBody           string
	issueApprovers      []string
	disallowedUsers     []string
	minimumApprovals    int
	workflowInitiator   string
}

func newApprovalEnvironment(client *github.Client, repoFullName, repoOwner string, runID int, approvers []string, minimumApprovals int, issueTitle, issueBody string, disallowedUsers []string, workflowInitiator string) (*approvalEnvironment, error) {
	repoOwnerAndName := strings.Split(repoFullName, "/")
	if len(repoOwnerAndName) != 2 {
		return nil, fmt.Errorf("repo owner and name in unexpected format: %s", repoFullName)
	}
	repo := repoOwnerAndName[1]

	return &approvalEnvironment{
		client:            client,
		repoFullName:      repoFullName,
		repo:              repo,
		repoOwner:         repoOwner,
		runID:             runID,
		issueApprovers:    approvers,
		disallowedUsers:   disallowedUsers,
		minimumApprovals:  minimumApprovals,
		issueTitle:        fmt.Sprintf("Manual approval required for: %s (run %d)", issueTitle, runID),
		issueBody:         issueBody,
		workflowInitiator: workflowInitiator,
	}, nil
}

func (a approvalEnvironment) runURL() string {
	return fmt.Sprintf("%s%s/actions/runs/%d", githubBaseURL, a.repoFullName, a.runID)
}

func (a *approvalEnvironment) createApprovalIssue(ctx context.Context) error {
	issueApproversText := "Anyone can approve."
	assignees := []string{a.workflowInitiator}
	if len(a.issueApprovers) > 0 {
		issueApproversText = fmt.Sprintf("%s", a.issueApprovers)
		assignees = a.issueApprovers
	}

	issueBody := fmt.Sprintf(`Workflow is pending manual review.
URL: %s

Required approvers: %s

Respond %s to continue workflow or %s to cancel.`,
		a.runURL(),
		issueApproversText,
		formatAcceptedWords(approvedWords),
		formatAcceptedWords(deniedWords),
	)

	if a.issueBody != "" {
		issueBody = fmt.Sprintf("%s\n\n%s", a.issueBody, issueBody)
	}

	var err error
	fmt.Printf(
		"Creating issue in repo %s/%s with the following content:\nTitle: %s\nApprovers: %s\nBody:\n%s\n",
		a.repoOwner,
		a.repo,
		a.issueTitle,
		assignees,
		issueBody,
	)
	a.approvalIssue, _, err = a.client.Issues.Create(ctx, a.repoOwner, a.repo, &github.IssueRequest{
		Title:     &a.issueTitle,
		Body:      &issueBody,
		Assignees: &assignees,
	})
	if err != nil {
		return err
	}
	a.approvalIssueNumber = a.approvalIssue.GetNumber()

	fmt.Printf("Issue created: %s\n", a.approvalIssue.GetURL())
	return nil
}

func approvalFromComments(comments []*github.IssueComment, approvers []string, minimumApprovals int, disallowedUsers []string) (approvalStatus, error) {

	approvals := []string{}

	if minimumApprovals == 0 {
		if len(approvers) == 0 {
			return "", fmt.Errorf("error: no required approvers or minimum approvals set")
		}
		minimumApprovals = len(approvers)
	}

	for _, comment := range comments {
		commentUser := comment.User.GetLogin()

		if approversIndex(disallowedUsers, commentUser) >= 0 {
			continue
		}
		if approversIndex(approvals, commentUser) >= 0 {
			continue
		}
		if len(approvers) > 0 && approversIndex(approvers, commentUser) < 0 {
			continue
		}

		commentBody := comment.GetBody()
		isApprovalComment, err := isApproved(commentBody)
		if err != nil {
			return approvalStatusPending, err
		}
		if isApprovalComment {
			approvals = append(approvals, commentUser)
			if len(approvals) >= minimumApprovals {
				return approvalStatusApproved, nil
			}
			continue
		}

		isDenialComment, err := isDenied(commentBody)
		if err != nil {
			return approvalStatusPending, err
		}
		if isDenialComment {
			return approvalStatusDenied, nil
		}
	}

	return approvalStatusPending, nil
}

func approversIndex(approvers []string, name string) int {
	for idx, approver := range approvers {
		if approver == name {
			return idx
		}
	}
	return -1
}

func isApproved(commentBody string) (bool, error) {
	for _, approvedWord := range approvedWords {
		re, err := regexp.Compile(fmt.Sprintf("(?i)^%s[.!]*\n*\\s*$", approvedWord))
		if err != nil {
			fmt.Printf("Error parsing. %v", err)
			return false, err
		}

		matched := re.MatchString(commentBody)

		if matched {
			return true, nil
		}
	}

	return false, nil
}

func isDenied(commentBody string) (bool, error) {
	for _, deniedWord := range deniedWords {
		re, err := regexp.Compile(fmt.Sprintf("(?i)^%s[.!]*\n*\\s*$", deniedWord))
		if err != nil {
			fmt.Printf("Error parsing. %v", err)
			return false, err
		}
		matched := re.MatchString(commentBody)
		if matched {
			return true, nil
		}
	}

	return false, nil
}

func formatAcceptedWords(words []string) string {
	var quotedWords []string

	for _, word := range words {
		quotedWords = append(quotedWords, fmt.Sprintf("\"%s\"", word))
	}

	return strings.Join(quotedWords, ", ")
}
