import base64
import copy
import json
from pathlib import Path
import unittest
import urllib.error

import reuse

SHA = "a" * 40
SOURCE = b"test workflow source"
REPO = reuse.REPOSITORY
PREFIX = "/repos/" + REPO
ENV = {"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v1.2.3",
       "GITHUB_REPOSITORY": REPO, "GITHUB_SHA": SHA}


class API:
    def __init__(self):
        self.repo = {"full_name": REPO, "id": 5, "default_branch": "master"}
        self.workflow = {"id": 8, "path": reuse.WORKFLOW, "state": "active"}
        self.source = {"type": "file", "path": reuse.WORKFLOW, "encoding": "base64",
                       "content": base64.b64encode(SOURCE).decode()}
        self.run = {"id": 11, "workflow_id": 8, "path": reuse.WORKFLOW, "head_sha": SHA,
                    "event": "push", "head_branch": "master", "repository": self.repo.copy(),
                    "head_repository": self.repo.copy(), "run_attempt": 2,
                    "status": "completed", "conclusion": "success"}
        self.jobs = [{"name": "test", "run_id": 11, "head_sha": SHA, "status": "completed",
                      "conclusion": "success", "steps": [
                          {"name": name, "status": "completed", "conclusion": "success"}
                          for name in sorted(reuse.REQUIRED_STEPS)]}]
        self.runs = [self.run]
        self.paths = []
        self.after_first_run = None
        self.after_first_list = None

    def __call__(self, path):
        self.paths.append(path)
        if path == PREFIX:
            return self.repo
        if path == PREFIX + "/actions/workflows/ci.yml":
            return self.workflow
        if path == PREFIX + "/contents/" + reuse.WORKFLOW + "?ref=" + SHA:
            return self.source
        if path.startswith(PREFIX + "/actions/workflows/8/runs?"):
            result = {"total_count": len(self.runs), "workflow_runs": copy.deepcopy(self.runs)}
            if self.after_first_list:
                self.after_first_list(self)
                self.after_first_list = None
            return result
        if path == PREFIX + "/actions/runs/11":
            result = copy.deepcopy(self.run)
            if self.after_first_run:
                self.after_first_run(self)
                self.after_first_run = None
            return result
        if path == PREFIX + "/actions/runs/11/attempts/2/jobs?per_page=100&page=1":
            return {"total_count": len(self.jobs), "jobs": self.jobs}
        raise AssertionError("unexpected API path: " + path)


class ReuseTests(unittest.TestCase):
    def assertFallback(self, api, env=ENV, source=SOURCE, head=SHA):
        accepted, reason = reuse.decide(api, env, source, head)
        self.assertFalse(accepted, reason)

    def test_accept_exact_master_success_latest_attempt(self):
        api = API()
        self.assertTrue(reuse.decide(api, ENV, SOURCE, SHA)[0])
        self.assertEqual(api.paths.count(PREFIX + "/actions/runs/11"), 2)
        self.assertTrue(any("/attempts/2/jobs?" in path for path in api.paths))

    def test_wrong_run_identity_event_or_result(self):
        for key, value in {"id": 12, "workflow_id": 9, "path": "evil.yml", "head_sha": "b" * 40,
                           "event": "workflow_dispatch", "head_branch": "feature", "status": "in_progress",
                           "conclusion": "failure", "run_attempt": 0}.items():
            with self.subTest(key=key):
                api = API()
                api.run[key] = value
                self.assertFallback(api)
        for event in ("pull_request", "pull_request_target", "workflow_run"):
            api = API()
            api.run["event"] = event
            self.assertFallback(api)

    def test_wrong_repository_and_fork_metadata(self):
        for key in ("repository", "head_repository"):
            for field, value in (("full_name", "attacker/captain"), ("id", 999)):
                api = API()
                api.run[key][field] = value
                self.assertFallback(api)
        api = API()
        api.repo["default_branch"] = "elsewhere"
        self.assertFallback(api)
        self.assertFallback(API(), dict(ENV, GITHUB_REPOSITORY="attacker/captain"))

    def test_api_redirect_is_refused(self):
        with self.assertRaises(reuse.Untrusted):
            reuse.NoRedirect().redirect_request(None, None, 302, "redirect", {}, "https://example.com")

    def test_workflow_source_and_identity(self):
        for key, value in (("path", "other.yml"), ("state", "disabled_manually")):
            api = API()
            api.workflow[key] = value
            self.assertFallback(api)
        self.assertFallback(API(), source=b"changed validation configuration")
        self.assertFallback(API(), head="b" * 40)

    def test_missing_failed_skipped_and_extra_jobs_or_steps(self):
        for conclusion in ("failure", "skipped", "cancelled", None):
            for level in ("job", "step"):
                api = API()
                target = api.jobs[0] if level == "job" else api.jobs[0]["steps"][0]
                target["conclusion"] = conclusion
                self.assertFallback(api)
        api = API()
        api.jobs[0]["steps"].pop()
        self.assertFallback(api)
        api = API()
        api.jobs.append(dict(api.jobs[0], name="future-job", conclusion="skipped"))
        self.assertFallback(api)

    def test_no_old_green_when_latest_run_or_attempt_is_bad(self):
        api = API()
        api.runs.append({"id": 10})
        api.run["conclusion"] = "failure"
        self.assertFallback(api)
        api = API()
        api.after_first_run = lambda item: item.run.update(run_attempt=3)
        self.assertFallback(api)
        api = API()
        api.after_first_list = lambda item: item.runs.append({"id": 12})
        self.assertFallback(api)
        api = API()
        api.runs.append({"id": 12})  # A newer run without complete evidence.
        self.assertFallback(api)
        api = API()
        api.after_first_list = lambda item: item.runs.__setitem__(0, dict(item.run, run_attempt=3))
        self.assertFallback(api)

    def test_manual_release_and_non_tag_never_reuse(self):
        for env in (dict(ENV, GITHUB_EVENT_NAME="workflow_dispatch"), dict(ENV, GITHUB_REF="refs/heads/master")):
            api = API()
            self.assertFallback(api, env)
            self.assertEqual(api.paths, [])

    def test_network_malformed_and_no_runs_fail_closed(self):
        for error in (TimeoutError(), urllib.error.HTTPError("https://api.github.com", 403, "limited", {}, None),
                      json.JSONDecodeError("bad", "x", 0)):
            def broken(path):
                raise error
            self.assertFallback(broken)
        api = API()
        api.runs = []
        self.assertFallback(api)

    def test_pagination_and_incomplete_counts(self):
        paths = []
        def api(path):
            paths.append(path)
            rows = list(range(100)) if path.endswith("&page=1") else [100]
            return {"total_count": 101, "jobs": rows}
        self.assertEqual(reuse.pages(api, "/repos/example/repo/jobs", "jobs"), list(range(101)))
        self.assertTrue(paths[-1].endswith("page=2"))
        for response in ({"total_count": 2, "jobs": [1]}, {"total_count": 1001, "jobs": []}, {}):
            with self.assertRaises(reuse.Untrusted):
                reuse.pages(lambda _: response, "/repos/example/repo/jobs", "jobs")

    def test_all_required_checks_are_named_in_real_workflow(self):
        workflow = Path(reuse.WORKFLOW).read_text()
        for name in reuse.REQUIRED_STEPS:
            self.assertIn("- name: " + name + "\n", workflow)
        self.assertIn("go test -race -timeout 60m -coverpkg=./... -coverprofile=coverage.out ./...", workflow)


if __name__ == "__main__":
    unittest.main()
