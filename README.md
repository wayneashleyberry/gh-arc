## gh-arc

> ✨ A GitHub (gh) CLI extension to find archived dependencies.

### Why?

You have dependencies, and sometimes those dependencies are no longer maintained—their repositories become archived on GitHub. Archived repositories are read-only, will not receive updates, bug fixes, or security patches, and may eventually disappear. Knowing which of your dependencies are archived helps you:

- Identify potential risks in your project due to unmaintained code
- Make informed decisions about replacing or forking dependencies
- Avoid surprises from sudden removals or vulnerabilities

Staying aware of archived dependencies is an important part of keeping your project healthy and secure.

### Installation

Installation is a single command if you already have the [GitHub CLI](https://cli.github.com) installed:

```sh
gh extension install wayneashleyberry/gh-arc
```

The [GitHub CLI](https://cli.github.com) also manages updates:

```sh
gh extension upgrade --all
```

### Usage

#### List Archived Go Modules

```sh
gh arc gomod
```

#### Help

```sh
gh arc help
```

```
NAME:
   arc - List archived dependencies

USAGE:
   arc [global options] command [command options]

COMMANDS:
   gomod    List archived go modules
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --debug     Print debug logs (default: false)
   --help, -h  show help
```
