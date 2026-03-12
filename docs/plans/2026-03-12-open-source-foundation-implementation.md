# Open Source Foundation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the minimum repository-level open source foundation files for Codex Gateway using GPL-3.0 and align the README with those new project policies.

**Architecture:** This change is documentation-only and centers on repository metadata. Add a standard GPL-3.0 license, contributor/security guidance, and wire those documents into the README so external readers can understand usage rights, reporting paths, and contribution expectations without inspecting code.

**Tech Stack:** Markdown, plain text, gitignore rules

---

### Task 1: Add repository license metadata

**Files:**
- Create: `LICENSE`

**Step 1: Write the license text**

Add the full GPL-3.0 license text with the current copyright holder line.

**Step 2: Verify the file exists**

Run: `test -f LICENSE && echo ok`
Expected: `ok`

### Task 2: Add contributor guidance

**Files:**
- Create: `CONTRIBUTING.md`

**Step 1: Write contribution guidelines**

Cover issue reporting, small focused PRs, documentation/testing expectations, and a reminder not to commit secrets or local runtime artifacts.

**Step 2: Verify the file exists**

Run: `test -f CONTRIBUTING.md && echo ok`
Expected: `ok`

### Task 3: Add security reporting guidance

**Files:**
- Create: `SECURITY.md`

**Step 1: Write the security policy**

Explain that security issues should not be opened publicly, define the temporary private disclosure path, and mention secret-handling expectations for configs and OAuth credentials.

**Step 2: Verify the file exists**

Run: `test -f SECURITY.md && echo ok`
Expected: `ok`

### Task 4: Align repository ignore and README references

**Files:**
- Modify: `README.md`
- Modify: `.gitignore`

**Step 1: Update README**

Add sections for License, Contributing, and Security, and point readers to the new documents.

**Step 2: Tighten ignore rules if needed**

Keep local config, credentials, and runtime artifacts out of version control.

**Step 3: Verify references**

Run: `rg -n "CONTRIBUTING|SECURITY|GPL-3.0|License" README.md`
Expected: matches for all new sections

### Task 5: Sanity-check repository state

**Files:**
- Modify: none

**Step 1: Review diff**

Run: `git diff -- LICENSE CONTRIBUTING.md SECURITY.md README.md .gitignore`
Expected: only documentation and ignore-rule changes

**Step 2: Confirm sensitive paths remain ignored**

Run: `git check-ignore -v config.yaml credentials/openai-oauth.json`
Expected: both paths reported as ignored
