package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-github/v74/github"
)

func retrieveApprovers(client *github.Client, repoOwner string) ([]string, []string, int, error) {
	workflowInitiator := os.Getenv(envVarWorkflowInitiator)
	shouldExcludeWorkflowInitiatorRaw := os.Getenv(envVarExcludeWorkflowInitiatorAsApprover)
	shouldExcludeWorkflowInitiator, parseBoolErr := strconv.ParseBool(shouldExcludeWorkflowInitiatorRaw)
	if parseBoolErr != nil {
		return nil, nil, 0, fmt.Errorf("error parsing exclude-workflow-initiator-as-approver flag: %w", parseBoolErr)
	}

	var approvers []string
	requiredApproversRaw := os.Getenv(envVarApprovers)
	var requiredApprovers []string
	if requiredApproversRaw != "" {
		requiredApprovers = strings.Split(requiredApproversRaw, ",")
	}

	var disallowedUsers []string
	if shouldExcludeWorkflowInitiator {
		disallowedUsers = []string{workflowInitiator}
	}

	if len(requiredApprovers) == 0 {
		return nil, disallowedUsers, 0, nil
	}

	for i := range requiredApprovers {
		requiredApprovers[i] = strings.TrimSpace(requiredApprovers[i])
	}

	for _, approverUser := range requiredApprovers {
		expandedUsers, err := expandGroupFromUser(client, repoOwner, approverUser, workflowInitiator, shouldExcludeWorkflowInitiator)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error resolving approver %q: %w", approverUser, err)
		}
		if expandedUsers != nil {
			approvers = append(approvers, expandedUsers...)
		} else if strings.EqualFold(workflowInitiator, approverUser) && shouldExcludeWorkflowInitiator {
			fmt.Printf("Not adding user '%s' as an approver as they are the workflow initiator\n", approverUser)
		} else {
			approvers = append(approvers, approverUser)
		}
	}

	approvers = deduplicateUsers(approvers)

	minimumApprovals := len(approvers)
	if raw := os.Getenv(envVarMinimumApprovals); raw != "" {
		var err error
		minimumApprovals, err = strconv.Atoi(raw)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error parsing minimum number of approvals: %w", err)
		}
	}

	if minimumApprovals > len(approvers) {
		return nil, nil, 0, fmt.Errorf("error: minimum required approvals (%d) is greater than the total number of approvers (%d)", minimumApprovals, len(approvers))
	}

	return approvers, disallowedUsers, minimumApprovals, nil
}

func expandGroupFromUser(client *github.Client, org, userOrTeam string, workflowInitiator string, shouldExcludeWorkflowInitiator bool) ([]string, error) {
	fmt.Printf("Attempting to expand user %s/%s as a group (may not succeed)\n", org, userOrTeam)

	// GitHub replaces periods in the team name with hyphens. If a period is
	// passed to the request it would result in a 404. So we need to replace
	// any occurrences with a hyphen.
	formattedUserOrTeam := strings.ReplaceAll(userOrTeam, ".", "-")

	users, resp, err := client.Teams.ListTeamMembersBySlug(context.Background(), org, formattedUserOrTeam, &github.TeamListTeamMembersOptions{})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, nil
		}
		return nil, fmt.Errorf("error expanding team %s/%s: %w", org, userOrTeam, err)
	}

	userNames := make([]string, 0, len(users))
	for _, user := range users {
		userName := user.GetLogin()
		if strings.EqualFold(userName, workflowInitiator) && shouldExcludeWorkflowInitiator {
			fmt.Printf("Not adding user '%s' from group '%s' as an approver as they are the workflow initiator\n", userName, userOrTeam)
		} else {
			userNames = append(userNames, userName)
		}
	}

	return userNames, nil
}

func deduplicateUsers(users []string) []string {
	uniqValuesByKey := make(map[string]bool)
	uniqUsers := []string{}
	for _, user := range users {
		if _, ok := uniqValuesByKey[user]; !ok {
			uniqValuesByKey[user] = true
			uniqUsers = append(uniqUsers, user)
		}
	}
	return uniqUsers
}
