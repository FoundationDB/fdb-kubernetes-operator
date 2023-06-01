#!/usr/bin/env python3
"""
fetch recent PRs from GitHub API and stuff into a version specific changelog file

this script requires PyGithub (pip3 install PyGithub)
"""
import logging
import os
import github
import github.GithubException


def get_github_key_from_file():
    """
    fetch a GitHub Personal Access Token from "~/.creds/github_oauth"
    using some python os path niceness, return it as a string.
    :return:
    """
    credentials_file_path = os.path.join(
        os.path.expanduser("~"), ".creds", "github_oauth"
    )
    with open(credentials_file_path, encoding="utf-8") as credentials_file:
        pat = credentials_file.read().rstrip("\n")
    return pat


LOG = logging.getLogger(__name__)
logging.basicConfig(format="[%(filename)s:%(lineno)s %(funcName)30s()] %(message)s")
LOG.setLevel(logging.DEBUG)
GITHUB_TOKEN = get_github_key_from_file()
GITHUB = github.Github(GITHUB_TOKEN)
GITHUB_OWNER = "FoundationDB"
GITHUB_REPO = "fdb-kubernetes-operator"


def get_latest_release_version():
    """
    get the tag name for the latest release version
    :return:
    """
    repo = GITHUB.get_user(GITHUB_OWNER).get_repo(GITHUB_REPO)
    latest_release_version = repo.get_latest_release().tag_name
    return latest_release_version


def calculate_new_release_version(version):
    """
    this is dumb, but works for the most part, it will fail for anything other
    than a minor release. (e.g. major 1.x.x --> 2.x.x or patch 1.17.0 --> 1.17.1
    cases are not covered)
    :param version:
    :return:
    """
    split_version_string = version.split(".")
    part_zero = split_version_string[0]
    part_one = int(split_version_string[1]) + 1
    part_two = split_version_string[2]
    new_version = f"{part_zero}.{part_one}.{part_two}"
    return new_version


def get_pull_requests():
    """
    get all closed PRs against main and filter for items
    closed after get_latest_release_commit_date(), drafts, and NOT merged,
    return a list
    :return:
    """
    pull_requests = []
    url_base = "https://github.pie.apple.com/FoundationDB/fdb-automation/pull/"
    repo = GITHUB.get_user(GITHUB_OWNER).get_repo(GITHUB_REPO)
    pulls = repo.get_pulls(base="main", state="closed")
    last_release_date = repo.get_latest_release().published_at
    for pull in pulls:
        if not pull.draft and pull.closed_at > last_release_date and pull.merged:
            log_line = f"* {pull.title} [#{pull.number}]({url_base}{pull.number})"
            pull_requests.append(log_line)
    return pull_requests


def update_changelog_file(log_lines, version):
    """
    take a list (of str) and put it on the top of the docs/changelog/{version}.md
    :param log_lines:
    :param version:

    the path for docs/changelog.md expects that this script is run from the
    project root, not from the scripts directory
    :return:
    """
    with open(
        f"docs/changelog/{version}.md", "w+", encoding="utf-8"
    ) as change_log_file:
        for line in log_lines:
            LOG.info(line)
            change_log_file.write(line + "\n")


if __name__ == "__main__":
    current_version = get_latest_release_version()
    new_release_version = calculate_new_release_version(current_version)
    change_log_lines = [
        f"# {new_release_version}",
        "",
        "## Changes",
        "",
        "### Operator",
        "",
    ] + get_pull_requests()
    update_changelog_file(change_log_lines, new_release_version)
