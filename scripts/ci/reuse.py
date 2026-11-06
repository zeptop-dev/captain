#!/usr/bin/env python3
"""Reuse only the latest successful master-push CI for this exact release commit.

This reads GitHub metadata and the workflow source, never CI artifacts. Any missing,
ambiguous or unavailable evidence requests the normal full release test suite.
"""
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.parse
import urllib.request

WORKFLOW = ".github/workflows/ci.yml"
REPOSITORY = "zeptop-dev/captain"
REQUIRED_STEPS = {
    "Check release CI helpers", "Check translations", "Build and test admin",
    "Build and lint portal", "Build and test site", "Build probe",
    "Check Go formatting", "Go vet", "Go vulnerability scan",
    "Go race and coverage", "Report coverage",
}


class Untrusted(ValueError):
    pass


def require(value):
    if not value:
        raise Untrusted("incomplete or mismatched CI evidence")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise Untrusted("GitHub API redirect refused")


class GitHub:
    def __init__(self, token):
        require(token)
        self.token = token
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), NoRedirect())

    def __call__(self, path):
        require(path.startswith("/repos/"))
        request = urllib.request.Request("https://api.github.com" + path, headers={
            "Accept": "application/vnd.github+json",
            "Authorization": "Bearer " + self.token,
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "captain-release-ci-check",
        })
        with self.opener.open(request, timeout=15) as response:
            payload = response.read(8 * 1024 * 1024 + 1)
        require(len(payload) <= 8 * 1024 * 1024)
        return json.loads(payload)


def pages(api, path, key):
    result = []
    separator = "&" if "?" in path else "?"
    for page in range(1, 11):
        data = api(f"{path}{separator}per_page=100&page={page}")
        require(isinstance(data.get("total_count"), int) and data["total_count"] <= 1000)
        rows = data.get(key)
        require(isinstance(rows, list) and len(rows) <= 100)
        result.extend(rows)
        if len(rows) < 100:
            require(len(result) == data["total_count"])
            return result
    raise Untrusted("pagination limit reached")


def trusted_run(api, repository, sha, workflow_source):
    require(repository == REPOSITORY and re.fullmatch(r"[0-9a-f]{40}", sha))
    prefix = f"/repos/{repository}"
    repo = api(prefix)
    require(repo.get("full_name") == repository and repo.get("default_branch") == "master")
    require(isinstance(repo.get("id"), int) and repo["id"] > 0)
    workflow = api(f"{prefix}/actions/workflows/ci.yml")
    require(workflow.get("path") == WORKFLOW and workflow.get("state") == "active")
    require(isinstance(workflow.get("id"), int) and workflow["id"] > 0)
    source = api(f"{prefix}/contents/{WORKFLOW}?ref={sha}")
    require(source.get("type") == "file" and source.get("path") == WORKFLOW)
    require(source.get("encoding") == "base64")
    require(base64.b64decode(source["content"].replace("\n", ""), validate=True) == workflow_source)
    runs_path = (f"{prefix}/actions/workflows/{workflow['id']}/runs?"
                 + urllib.parse.urlencode({"head_sha": sha, "event": "push", "branch": "master"}))
    runs = pages(api, runs_path, "workflow_runs")
    require(runs)
    ids = [run.get("id") for run in runs]
    require(all(isinstance(value, int) and value > 0 for value in ids))
    require(len(set(ids)) == len(ids))
    # A newer pending/failed run or a failed rerun must not be hidden by an old green run.
    run_id = max(ids)
    path = f"{prefix}/actions/runs/{run_id}"
    run = api(path)

    def check_run(candidate):
        require(candidate.get("id") == run_id and candidate.get("workflow_id") == workflow["id"])
        require(candidate.get("path") == WORKFLOW and candidate.get("head_sha") == sha)
        require(candidate.get("event") == "push" and candidate.get("head_branch") == "master")
        for key in ("repository", "head_repository"):
            require(candidate.get(key, {}).get("id") == repo["id"])
            require(candidate.get(key, {}).get("full_name") == repository)
        require(candidate.get("status") == "completed" and candidate.get("conclusion") == "success")
        require(isinstance(candidate.get("run_attempt"), int) and candidate["run_attempt"] > 0)

    check_run(run)
    attempt = run["run_attempt"]
    jobs = pages(api, f"{path}/attempts/{attempt}/jobs", "jobs")
    # This contract intentionally fails closed if CI is split/renamed later.
    require(len(jobs) == 1 and jobs[0].get("name") == "test")
    job = jobs[0]
    require(job.get("run_id") == run_id and job.get("head_sha") == sha)
    require(job.get("status") == "completed" and job.get("conclusion") == "success")
    steps = job.get("steps", [])
    require(steps and all(step.get("status") == "completed" and step.get("conclusion") == "success" for step in steps))
    names = [step.get("name") for step in steps]
    require(len(names) == len(set(names)) and REQUIRED_STEPS <= set(names))
    latest = api(path)
    check_run(latest)
    require(latest["run_attempt"] == attempt)
    # Recheck the list too, in case another push run appeared during inspection.
    latest_runs = pages(api, runs_path, "workflow_runs")
    require(latest_runs and max(item["id"] for item in latest_runs) == run_id)
    listed = next(item for item in latest_runs if item["id"] == run_id)
    check_run(listed)
    require(listed["run_attempt"] == attempt)
    return run_id, attempt


def decide(api, env, workflow_source, head):
    if env.get("GITHUB_EVENT_NAME") != "push" or not env.get("GITHUB_REF", "").startswith("refs/tags/v"):
        return False, "Manual/non-tag run: full release validation required."
    try:
        require(head == env.get("GITHUB_SHA"))
        run_id, attempt = trusted_run(api, env.get("GITHUB_REPOSITORY"), head, workflow_source)
        return True, f"Reusing exact-commit master CI run {run_id}, attempt {attempt}."
    except Exception:
        # Never print remote error bodies, headers or credentials. Failure is safe:
        # the caller retains every existing full-suite assertion and time budget.
        return False, "No complete trusted CI evidence: full release validation required."


def main():
    try:
        head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
        source = Path(WORKFLOW).read_bytes()
        api = GitHub(os.environ.get("GH_TOKEN", ""))
        reuse, reason = decide(api, os.environ, source, head)
    except Exception:
        reuse, reason = False, "CI lookup unavailable: full release validation required."
    print(reason)
    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with open(output, "a", encoding="utf-8") as file:
            file.write(f"reuse={'true' if reuse else 'false'}\n")
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as file:
            file.write(reason + "\n")


if __name__ == "__main__":
    main()
