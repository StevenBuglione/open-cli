# Monorepo Consolidation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the standalone `oas-cli-spec` and `oas-cli-conformance` repositories into `open-cli` as root-level sibling subprojects so the project is maintained from one repository going forward.

**Architecture:** Preserve the Go implementation as the repo root while importing the spec and conformance repositories as first-class top-level directories named `spec/` and `conformance/`. Keep spec and conformance independently understandable and runnable, then rewire the root CI, verification commands, and documentation so contract verification happens in-repo instead of across repositories.

**Tech Stack:** Git subtree import, Go, Python, GitHub Actions, Docusaurus, Make

---

## File map

- Create: `spec/` (import of the current `oas-cli-spec` repository)
- Create: `conformance/` (import of the current `oas-cli-conformance` repository)
- Create: `docs/superpowers/plans/2026-03-14-monorepo-consolidation.md`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `.github/workflows/ci.yml`
- Modify: `website/docs/development/repo-layout.md`
- Modify: `website/docs/development/testing.md`
- Modify: `website/docs/configuration/config-schema.md`
- Modify: `docs/superpowers/specs/2026-03-14-mcp-native-oauth-design.md`
- Modify: session plan file `~/.copilot/session-state/7a79c9cc-b5c5-4668-8979-f3899a2d9a01/plan.md`

## Chunk 1: Import the companion repositories as subprojects

### Task 1: Import `oas-cli-spec` into `spec/`

**Files:**
- Create: `spec/README.md`
- Create: `spec/spec/`
- Create: `spec/schemas/`
- Create: `spec/examples/`
- Create: `spec/scripts/`
- Verify: `git log --oneline -- spec`

- [ ] **Step 1: Add the local spec repo as a temporary remote**

Run:

```bash
cd /home/sbuglione/ocli/open-cli/.worktrees/monorepo-consolidation
git remote add spec-local /home/sbuglione/ocli/oas-cli-spec
```

- [ ] **Step 2: Import the repo with history preserved**

Run:

```bash
git subtree add --prefix=spec spec-local main
```

Expected: `spec/` appears with the current spec repository contents and subtree merge history.

- [ ] **Step 3: Remove the temporary remote**

Run:

```bash
git remote remove spec-local
```

- [ ] **Step 4: Verify the imported tree**

Run:

```bash
test -f spec/schemas/cli.schema.json
test -f spec/spec/core.md
git log --oneline -- spec | head
```

Expected: files exist and `git log` shows imported history for `spec/`.

### Task 2: Import `oas-cli-conformance` into `conformance/`

**Files:**
- Create: `conformance/README.md`
- Create: `conformance/fixtures/`
- Create: `conformance/expected/`
- Create: `conformance/scripts/`
- Create: `conformance/tests/`
- Verify: `git log --oneline -- conformance`

- [ ] **Step 1: Add the local conformance repo as a temporary remote**

Run:

```bash
cd /home/sbuglione/ocli/open-cli/.worktrees/monorepo-consolidation
git remote add conformance-local /home/sbuglione/ocli/oas-cli-conformance
```

- [ ] **Step 2: Import the repo with history preserved**

Run:

```bash
git subtree add --prefix=conformance conformance-local main
```

Expected: `conformance/` appears with the current conformance repository contents and subtree merge history.

- [ ] **Step 3: Remove the temporary remote**

Run:

```bash
git remote remove conformance-local
```

- [ ] **Step 4: Verify the imported tree**

Run:

```bash
test -f conformance/compatibility-matrix.json
test -f conformance/scripts/run_conformance.py
git log --oneline -- conformance | head
```

Expected: files exist and `git log` shows imported history for `conformance/`.

## Chunk 2: Rewire verification and automation for the monorepo

### Task 3: Update root verification commands

**Files:**
- Modify: `Makefile`
- Verify: `make verify`, `make verify-spec`, `make verify-conformance`, `make verify-all`

- [ ] **Step 1: Add failing monorepo verification commands**

Edit `Makefile` to add targets for:

```make
verify-spec:
	cd spec && python3 -m pip install -r requirements.txt && python3 scripts/validate_examples.py

verify-conformance:
	cd conformance && python3 -m pip install -r requirements.txt && python3 scripts/run_conformance.py --schema-root ../spec/schemas

verify-all: verify verify-spec verify-conformance
```

- [ ] **Step 2: Run the new targets before fixing docs/CI references**

Run:

```bash
make verify-spec
make verify-conformance
```

Expected: at least one command fails if any path assumptions still point to the old multi-repo layout.

- [ ] **Step 3: Fix the implementation**

Adjust `Makefile` to use stable commands that work from the repo root and inside CI without depending on sibling repositories outside the checkout.

- [ ] **Step 4: Re-run the commands**

Run:

```bash
make verify
make verify-spec
make verify-conformance
make verify-all
```

Expected: all targets pass.

### Task 4: Update CI to use in-repo spec and conformance

**Files:**
- Modify: `.github/workflows/ci.yml`
- Inspect: `spec/.github/workflows/ci.yml`
- Inspect: `conformance/.github/workflows/ci.yml`

- [ ] **Step 1: Write the failing CI shape**

Edit `.github/workflows/ci.yml` so it contains dedicated jobs for:

```yaml
spec-validate:
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-python@v5
      with:
        python-version: "3.12"
    - run: cd spec && python3 -m pip install -r requirements.txt
    - run: cd spec && python3 scripts/validate_examples.py

conformance-validate:
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-python@v5
      with:
        python-version: "3.12"
    - run: cd conformance && python3 -m pip install -r requirements.txt
    - run: cd conformance && python3 scripts/run_conformance.py --schema-root ../spec/schemas
```

- [ ] **Step 2: Remove cross-repo checkout behavior**

Delete any `actions/checkout` step that fetches `StevenBuglione/oas-cli-spec` into the workspace. The monorepo checkout must be sufficient.

- [ ] **Step 3: Keep existing implementation and docs validation**

Retain the existing Go verification and docs build jobs from `open-cli`.

- [ ] **Step 4: Validate the workflow syntax locally**

Run:

```bash
python3 - <<'PY'
import yaml, pathlib
for path in [pathlib.Path('.github/workflows/ci.yml')]:
    yaml.safe_load(path.read_text())
print('workflow-ok')
PY
```

Expected: `workflow-ok`

## Chunk 3: Sync docs and contributor guidance to the new layout

### Task 5: Update repository and developer docs

**Files:**
- Modify: `README.md`
- Modify: `website/docs/development/repo-layout.md`
- Modify: `website/docs/development/testing.md`
- Modify: `website/docs/configuration/config-schema.md`
- Modify: `docs/superpowers/specs/2026-03-14-mcp-native-oauth-design.md`

- [ ] **Step 1: Write the failing documentation assertions**

Run:

```bash
rg -n "cross-repository|oas-cli-spec|oas-cli-conformance" README.md website/docs docs/superpowers/specs
```

Expected: existing references to the old split-repo model are found.

- [ ] **Step 2: Update root README**

Describe the repo as the single source of truth with top-level `spec/` and `conformance/` directories and in-repo verification commands.

- [ ] **Step 3: Update Docusaurus development docs**

Make these changes:

- `website/docs/development/repo-layout.md`: add `spec/` and `conformance/` to the top-level structure.
- `website/docs/development/testing.md`: replace “cross-repository” instructions with in-repo commands.
- `website/docs/configuration/config-schema.md`: point the public schema location at `spec/schemas/cli.schema.json` and clarify that `pkg/config/cli.schema.json` is the implementation copy.

- [ ] **Step 4: Update design/spec artifacts**

Edit `docs/superpowers/specs/2026-03-14-mcp-native-oauth-design.md` so it refers to the in-repo `spec/` and `conformance/` subprojects rather than separate repositories.

- [ ] **Step 5: Build the docs site**

Run:

```bash
cd website
npm ci
npm run build
```

Expected: the site builds successfully with the updated docs.

## Chunk 4: Verify, merge, and publish the monorepo change

### Task 6: Run full verification

**Files:**
- Verify: repo root plus imported subprojects

- [ ] **Step 1: Run the full verification bundle**

Run:

```bash
cd /home/sbuglione/ocli/open-cli/.worktrees/monorepo-consolidation
make verify-all
cd website && npm ci && npm run build
```

Expected: all verification passes from the monorepo checkout.

- [ ] **Step 2: Review git status and imported history**

Run:

```bash
git --no-pager status --short
git log --oneline -- spec | head -5
git log --oneline -- conformance | head -5
```

Expected: only intended files are modified and both imported trees show preserved history.

- [ ] **Step 3: Commit the work**

Run:

```bash
git add Makefile README.md .github/workflows/ci.yml website/docs docs/superpowers/specs spec conformance
git -c commit.gpgsign=false commit -m "feat: consolidate spec and conformance into monorepo

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

- [ ] **Step 4: Push and merge to `main`**

Run:

```bash
cd /home/sbuglione/ocli/open-cli/.worktrees/monorepo-consolidation
git push -u origin feature/monorepo-consolidation
cd /home/sbuglione/ocli/open-cli
git checkout main
git pull --ff-only origin main
git merge --no-ff feature/monorepo-consolidation
git push origin main
```

Expected: the feature branch is pushed, merged, and `origin/main` contains the monorepo layout.

- [ ] **Step 5: Confirm the new CI passes on `main`**

Run:

```bash
gh run list --repo StevenBuglione/open-cli --limit 5
```

Expected: the post-merge `ci` run on `main` succeeds with in-repo spec and conformance validation.
