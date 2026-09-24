# GitHub Org Clone

This project attempts to help users manage their repositories in organizations. It is often convenient to have
repositories available ahead of time, either for reference with coding agents or just to not have to re-clone
information.

Invoking the CLI should by default:
* Use the `gh` CLI to scan the provided GitHub organization for all repositories
* If the repository does not exist locally, clone it
* If it does, fetch all updates
* If it is archived, make it an archive on the machine (tarball), as well, with an indication of the last work done to
  the repository (this last part we need to figure out - commit hash? Tags? Updated timestamp?)

This should all land in one location - where is potentially up to the user (we should definitely allow configuration of
many of the behaviors noted above), but we should have a same default. This gives a single directory with all the
information the user could grasp from a GitHub organization.

When doing updates, the CLI should attempt to do as little work as possible. It should be tracking when no work needs to
be done, and attempt to use that information to stop checking other repositories.

This is a jujutsu repository. All work should get a dedicated jujutsu commit before work starts to partition off
concepts from each other and make everything easier to track.
