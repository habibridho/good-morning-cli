package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/habib-ridho/good-morning/internal/config"
	"github.com/habib-ridho/good-morning/internal/llm"
	"github.com/habib-ridho/good-morning/internal/messenger/lark"
	"github.com/habib-ridho/good-morning/internal/planner"
	"github.com/habib-ridho/good-morning/internal/tracker"
	"github.com/habib-ridho/good-morning/internal/tracker/jira"
)

var rootCmd = &cobra.Command{
	Use:   "good-morning",
	Short: "A CLI tool to generate morning team plans from Jira to Lark",
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the morning briefing generation and send to Lark",
	Run:   runBriefing,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactively set up configuration and map Jira lanes",
	Run:   runInit,
}

var dryRun bool

func init() {
	// Load .env automatically if it exists
	_ = godotenv.Load()

	runCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Print the generated message to console instead of sending to Lark")
	rootCmd.AddCommand(runCmd, initCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runBriefing(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error loading config: %v\nRun 'good-morning init' to set up configuration.", err)
	}

	if err := cfg.ValidateRun(); err != nil {
		log.Fatalf("Configuration missing: %v\nRun 'good-morning init' to set up configuration.", err)
	}

	ctx := context.Background()

	// 1. Warmup LLM
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMSystemPrompt, cfg.LLMTimeoutSec)
	go func() {
		if err := llmClient.Warmup(ctx); err != nil {
			log.Printf("LLM warmup failed (non-fatal): %v", err)
		}
	}()

	// 2. Fetch Sprint and Issues
	jiraClient := jira.NewClient(cfg.JiraBaseURL, cfg.JiraEmail, cfg.JiraToken, cfg.JiraBoardID)
	
	sprint, err := jiraClient.GetActiveSprint(ctx)
	if err != nil {
		log.Fatalf("Failed to get active sprint: %v", err)
	}
	log.Printf("Found active sprint: %s (ID: %d)", sprint.Name, sprint.ID)

	issues, err := jiraClient.GetSprintIssues(ctx, sprint.ID)
	if err != nil {
		log.Fatalf("Failed to get sprint issues: %v", err)
	}
	log.Printf("Fetched %d issues from Jira", len(issues))

	// 3. Prioritize and Plan
	plan := planner.Build(*sprint, issues, cfg.Members, cfg.LaneMapping)
	
	log.Printf("Generated plan for %d team members", len(plan.Assignments))

	// 4. Generate Message
	log.Println("Generating message with LLM...")
	message, err := llmClient.GenerateMorningMessage(ctx, plan)
	if err != nil {
		log.Fatalf("Failed to generate message: %v", err)
	}

	if dryRun {
		fmt.Println("\n--- DRY RUN MESSAGE ---")
		fmt.Println(message)
		fmt.Println("-----------------------")
		return
	}

	// 5. Send via Lark
	log.Println("Sending message to Lark...")
	larkClient := lark.NewClient(cfg.LarkAppID, cfg.LarkAppSecret, cfg.LarkGroupChatID)
	if err := larkClient.Authenticate(); err != nil {
		log.Fatalf("Failed to authenticate with Lark: %v", err)
	}

	if err := larkClient.SendMorningPlan(ctx, message, cfg.Members) ; err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	log.Println("Successfully sent morning briefing!")
}

func runInit(cmd *cobra.Command, args []string) {
	fmt.Println("Welcome to the good-morning setup wizard!")
	reader := bufio.NewReader(os.Stdin)

	// ── Step 1: Jira board link → base URL + board ID ───────────────────────
	fmt.Println("\n[1/5] Jira")
	fmt.Println("Paste your Jira board URL (e.g. https://company.atlassian.net/jira/software/projects/PROJ/boards/42):")

	defaultBoardLink := buildDefaultBoardLink(
		os.Getenv("GOOD_MORNING_JIRA_BASE_URL"),
		os.Getenv("GOOD_MORNING_JIRA_BOARD_ID"),
	)
	boardLink := prompt(reader, "Jira board URL", defaultBoardLink)

	jiraBaseURL, jiraBoardID, err := parseJiraBoardLink(boardLink)
	if err != nil {
		fmt.Printf("Could not parse board link (%v).\n", err)
		fmt.Println("Falling back to manual entry.")
		jiraBaseURL = prompt(reader, "Jira Base URL", os.Getenv("GOOD_MORNING_JIRA_BASE_URL"))
		idStr := prompt(reader, "Jira Board ID (numeric)", os.Getenv("GOOD_MORNING_JIRA_BOARD_ID"))
		jiraBoardID, _ = strconv.Atoi(idStr)
	} else {
		fmt.Printf("  ✓ Base URL : %s\n", jiraBaseURL)
		fmt.Printf("  ✓ Board ID : %d\n", jiraBoardID)
	}

	// ── Step 2: Jira PAT / API Token ──────────────────────────────────────────
	patURL := "https://id.atlassian.com/manage-profile/security/api-tokens"
	fmt.Printf("\nYou can create or copy an API Token (PAT) at:\n  %s\n", patURL)
	fmt.Println("(Navigate to the link above and click 'Create API token')")
	jiraEmail := prompt(reader, "Jira Email", os.Getenv("GOOD_MORNING_JIRA_EMAIL"))
	jiraToken := prompt(reader, "Jira PAT Token", os.Getenv("GOOD_MORNING_JIRA_TOKEN"))

	// ── Step 3: LLM ──────────────────────────────────────────────────────────
	fmt.Println("\n[2/5] LLM")
	llmURL := prompt(reader, "LLM Base URL", os.Getenv("GOOD_MORNING_LLM_BASE_URL"))
	llmModel := prompt(reader, fmt.Sprintf("LLM Model (default: %s)", llm.DefaultModel), os.Getenv("GOOD_MORNING_LLM_MODEL"))

	// ── Step 4: Lark ─────────────────────────────────────────────────────────
	fmt.Println("\n[3/5] Lark")
	larkAppID := prompt(reader, "Lark App ID", os.Getenv("GOOD_MORNING_LARK_APP_ID"))
	larkAppSecret := prompt(reader, "Lark App Secret", os.Getenv("GOOD_MORNING_LARK_APP_SECRET"))
	larkChatID := prompt(reader, "Lark Group Chat ID", os.Getenv("GOOD_MORNING_LARK_GROUP_CHAT_ID"))

	var larkClient *lark.Client
	if larkAppID != "" && larkAppSecret != "" {
		larkClient = lark.NewClient(larkAppID, larkAppSecret, larkChatID)
		if err := larkClient.Authenticate(); err != nil {
			fmt.Printf("Warning: failed to authenticate with Lark: %v\n", err)
			larkClient = nil
		}
	}

	// ── Step 5: Team member mapping from Jira → Lark ─────────────────────────
	fmt.Println("\n[4/5] Team Members")
	fmt.Println("Connecting to Jira to fetch team members from the active sprint...")

	jiraClient := jira.NewClient(jiraBaseURL, jiraEmail, jiraToken, jiraBoardID)
	ctx := context.Background()

	members := buildMembersFromExistingEnv(os.Getenv("GOOD_MORNING_TEAM_MEMBERS"))

	boardMembers, err := jiraClient.GetBoardMembers(ctx)
	if err != nil {
		fmt.Printf("Warning: could not fetch Jira users: %v\n", err)
		fmt.Println("Falling back to manual entry. Format: jira_account_id|lark_user_id|display_name|role")
		for {
			m := prompt(reader, "Add member (or leave blank to finish)", "")
			if m == "" {
				break
			}
			members = append(members, m)
		}
	} else if len(boardMembers) == 0 {
		fmt.Println("No assignees found in the active sprint. You can add members manually.")
		for {
			m := prompt(reader, "Add member (jira_account_id|lark_user_id|display_name|role, blank to finish)", "")
			if m == "" {
				break
			}
			members = append(members, m)
		}
	} else {
		fmt.Printf("Found %d unique assignee(s) in the active sprint.\n", len(boardMembers))
		fmt.Println("For each Jira user, enter their Lark email or user ID (leave blank to skip).")
		fmt.Println("(Tip: Lark user IDs look like ou_xxxxxxxxxxxxxxxxxxxxxxxx)")

		// Keep existing mappings keyed by Jira account ID so re-running init preserves them.
		existingByJiraID := parseExistingMembersByJiraID(os.Getenv("GOOD_MORNING_TEAM_MEMBERS"))

		var newMembers []string
		for _, bm := range boardMembers {
			defaultLark := ""
			defaultName := bm.DisplayName
			defaultRole := "developer"
			if ex, ok := existingByJiraID[bm.AccountID]; ok {
				defaultLark = ex.larkID
				if ex.name != "" {
					defaultName = ex.name
				}
				if ex.role != "" {
					defaultRole = ex.role
				}
			}

			fmt.Printf("\n  Jira user : %s (%s)\n", bm.DisplayName, bm.AccountID)
			if bm.Email != "" {
				fmt.Printf("  Jira email: %s\n", bm.Email)
			}

			var larkID string
			if defaultLark != "" {
				larkID = prompt(reader, "  Lark User ID (leave as is to keep)", defaultLark)
			} else {
				defaultInput := bm.Email
				input := prompt(reader, "  Lark email or User ID (blank to skip)", defaultInput)
				if input == "" {
					continue
				}
				if strings.Contains(input, "@") {
					if larkClient != nil {
						fmt.Printf("  Looking up Lark user ID for %s...\n", input)
						ids, err := larkClient.GetUserIDsByEmails(ctx, []string{input})
						if err != nil {
							fmt.Printf("  Warning: failed to lookup email: %v\n", err)
						} else if ids[input] == "" {
							fmt.Printf("  Warning: Lark user ID not found for email %s\n", input)
						} else {
							larkID = ids[input]
							fmt.Printf("  ✓ Found Lark User ID: %s\n", larkID)
						}
					} else {
						fmt.Println("  Warning: Lark client not authenticated, cannot lookup email.")
					}
					if larkID == "" {
						larkID = prompt(reader, "  Please enter Lark User ID manually (blank to skip)", "")
						if larkID == "" {
							continue
						}
					}
				} else {
					larkID = input
				}
			}

			if larkID == "" {
				continue
			}
			displayName := prompt(reader, "  Display name", defaultName)
			role := prompt(reader, "  Role (developer/tester)", defaultRole)
			newMembers = append(newMembers, fmt.Sprintf("%s|%s|%s|%s", bm.AccountID, larkID, displayName, role))
		}
		if len(newMembers) > 0 {
			members = newMembers
		}
	}

	// ── Step 6: Board column → lane group mapping ─────────────────────────────
	fmt.Println("\n[5/5] Lane Mapping")
	fmt.Println("Connecting to Jira to fetch board columns...")
	columns, err := jiraClient.GetBoardColumns(ctx)
	if err != nil {
		fmt.Printf("Warning: Could not fetch Jira columns: %v\n", err)
		fmt.Println("Skipping interactive lane mapping.")
	}

	laneMapping := make(map[string]string)
	if err == nil {
		fmt.Println("\nMap Jira columns to Framework groups.")
		fmt.Println("Framework groups:")
		fmt.Println("[1] Ready to Deploy")
		fmt.Println("[2] Testing")
		fmt.Println("[3] Ready to Test")
		fmt.Println("[4] In Review")
		fmt.Println("[5] In Progress")
		fmt.Println("[6] To Do")
		fmt.Println("[0] Skip (treat as lowest priority)")

		for _, col := range columns {
			for {
				choice := prompt(reader, fmt.Sprintf("Map Jira column %q", col.Name), "0")
				c, err := strconv.Atoi(choice)
				if err != nil || c < 0 || c > 6 {
					fmt.Println("Invalid choice. Enter 0-6.")
					continue
				}
				if c != 0 {
					groupName := string(tracker.LaneGroupOrder[c-1])
					laneMapping[col.Name] = groupName
				}
				break
			}
		}
	}

	mappingJSON, _ := json.Marshal(laneMapping)

	// ── Save to .env ──────────────────────────────────────────────────────────
	envLines := []string{
		fmt.Sprintf("GOOD_MORNING_JIRA_BASE_URL=%s", jiraBaseURL),
		fmt.Sprintf("GOOD_MORNING_JIRA_EMAIL=%s", jiraEmail),
		fmt.Sprintf("GOOD_MORNING_JIRA_TOKEN=%s", jiraToken),
		fmt.Sprintf("GOOD_MORNING_JIRA_BOARD_ID=%d", jiraBoardID),
		fmt.Sprintf("GOOD_MORNING_LLM_BASE_URL=%s", llmURL),
		fmt.Sprintf("GOOD_MORNING_LLM_MODEL=%s", llmModel),
		fmt.Sprintf("GOOD_MORNING_LARK_APP_ID=%s", larkAppID),
		fmt.Sprintf("GOOD_MORNING_LARK_APP_SECRET=%s", larkAppSecret),
		fmt.Sprintf("GOOD_MORNING_LARK_GROUP_CHAT_ID=%s", larkChatID),
		fmt.Sprintf("GOOD_MORNING_TEAM_MEMBERS=%s", strings.Join(members, ",")),
		fmt.Sprintf("GOOD_MORNING_LANE_MAPPING=%s", string(mappingJSON)),
	}

	envData := strings.Join(envLines, "\n")
	if err := os.WriteFile(".env", []byte(envData), 0600); err != nil {
		log.Fatalf("Failed to write .env file: %v", err)
	}

	fmt.Println("\n✓ Configuration saved to .env file successfully!")
}

// parseJiraBoardLink extracts the base URL and numeric board ID from a Jira board URL.
// Supports formats like:
//   - https://company.atlassian.net/jira/software/projects/PROJ/boards/42
//   - https://company.atlassian.net/secure/RapidBoard.jspa?rapidView=42
func parseJiraBoardLink(rawURL string) (string, int, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", 0, fmt.Errorf("empty URL")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", 0, fmt.Errorf("URL missing scheme or host")
	}

	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	// Pattern 1: /boards/42
	boardsRe := regexp.MustCompile(`/boards/(\d+)`)
	if m := boardsRe.FindStringSubmatch(u.Path); len(m) == 2 {
		id, _ := strconv.Atoi(m[1])
		return baseURL, id, nil
	}

	// Pattern 2: ?rapidView=42
	if rv := u.Query().Get("rapidView"); rv != "" {
		id, err := strconv.Atoi(rv)
		if err == nil {
			return baseURL, id, nil
		}
	}

	return "", 0, fmt.Errorf("could not find board ID in URL %q", rawURL)
}

// buildDefaultBoardLink reconstructs a board link from saved env vars, so re-running
// init shows a sensible default for the board URL prompt.
func buildDefaultBoardLink(baseURL, boardIDStr string) string {
	if baseURL == "" || boardIDStr == "" {
		return ""
	}
	return fmt.Sprintf("%s/jira/software/boards/%s", strings.TrimRight(baseURL, "/"), boardIDStr)
}

// existingMember is a tiny helper for re-running init with pre-populated values.
type existingMember struct {
	larkID string
	name   string
	role   string
}

// parseExistingMembersByJiraID parses GOOD_MORNING_TEAM_MEMBERS into a map keyed by Jira account ID.
func parseExistingMembersByJiraID(raw string) map[string]existingMember {
	result := make(map[string]existingMember)
	for _, part := range strings.Split(raw, ",") {
		var fields []string
		if strings.Contains(part, "|") {
			fields = strings.Split(strings.TrimSpace(part), "|")
		} else {
			fields = strings.Split(strings.TrimSpace(part), ":")
		}
		if len(fields) >= 3 {
			role := "developer"
			if len(fields) == 4 {
				role = fields[3]
			}
			result[fields[0]] = existingMember{larkID: fields[1], name: fields[2], role: role}
		}
	}
	return result
}

// buildMembersFromExistingEnv splits the raw env value into a slice for later joining.
func buildMembersFromExistingEnv(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func prompt(r *bufio.Reader, label, defaultValue string) string {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	} else {
		fmt.Printf("%s: ", label)
	}
	
	input, _ := r.ReadString('\n')
	input = strings.TrimSpace(input)
	
	if input == "" {
		return defaultValue
	}
	return input
}
