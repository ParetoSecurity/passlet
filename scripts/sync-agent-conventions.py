"""Link pinned upstream conventions without overwriting project-owned files."""

from pathlib import Path
import sys

STORE_PREFIX = "/nix/store/"


def link(source: Path, target: Path) -> None:
    if target.is_symlink():
        if target.readlink() == source:
            return
        if not str(target.readlink()).startswith(STORE_PREFIX):
            raise SystemExit(f"Refusing to replace project-owned link: {target}")
        target.unlink()
    elif target.exists():
        raise SystemExit(f"Refusing to replace project-owned path: {target}")
    target.symlink_to(source, target_is_directory=True)


def sync(source: Path, project: Path) -> None:
    claude = project / ".claude"
    claude.mkdir(exist_ok=True)
    link(source / "rules", claude / "rules")
    skills = claude / "skills"
    skills.mkdir(exist_ok=True)
    desired = {entry.name: entry for entry in (source / "skills").iterdir() if entry.is_dir()}
    # Project-specific skill has precedence over any future upstream namesake.
    desired["passlet"] = project / "skills" / "passlet"
    for name, entry in desired.items():
        link(entry, skills / name)
    for entry in skills.iterdir():
        if entry.name not in desired and entry.is_symlink() and str(entry.readlink()).startswith(STORE_PREFIX):
            entry.unlink()


if __name__ == "__main__":
    sync(Path(sys.argv[1]), Path(sys.argv[2]))
