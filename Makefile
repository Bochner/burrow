# Human shortcuts only. Agents use Aspect directly; workflow logic lives there.
.DEFAULT_GOAL := help
WORKSPACE ?= $(HOME)/burrow-test
export BURROW_MAKE_WORKSPACE := $(WORKSPACE)

.PHONY: help run restart clean check
help:
	@echo 'make run      Build and open Burrow (default workspace: ~/burrow-test)'
	@echo 'make restart  Close connections and shells, then reopen without prompting'
	@echo 'make clean    Clear build outputs only; preserves workspaces and runtime'
	@echo 'make check    Run the portable gate'
	@echo 'Override workspace: make run WORKSPACE=/absolute/path'

run:
	aspect burrow run -- --workspace "$$BURROW_MAKE_WORKSPACE" tui

restart:
	aspect burrow run -- --workspace "$$BURROW_MAKE_WORKSPACE" restart --yes

clean:
	aspect burrow clean

check:
	aspect burrow-check ci
