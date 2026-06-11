# Good Morning CLI

A Go CLI tool that reads active Jira sprints, prioritizes tasks for each team member, and sends a morning briefing message to a Lark group chat using an LLM.

## Setup

First, build the CLI:
```bash
make build
```

Then, initialize the configuration interactively:
```bash
make init
```
This will guide you through setting up credentials for Jira, Lark, and LLM. It will also interactively map your Jira board columns to the internal priority framework.

## Usage

To generate and send the morning briefing to Lark:
```bash
make run
```

To dry-run (prints the LLM message to console instead of sending to Lark):
```bash
make run-dry
```

## Architecture

- **Project Tracker (`internal/tracker`)**: Interface to support various trackers. Currently supports Jira.
- **Planner (`internal/planner`)**: Core logic to prioritize issues based on Lane and Priority.
- **Messenger (`internal/messenger`)**: Interface to support various messaging platforms. Currently supports Lark.
- **LLM (`internal/llm`)**: LLM client to generate the humanized morning briefing message.
