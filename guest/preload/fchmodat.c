/*
 * libandronix-fchmodat.so: preloaded (/etc/ld.so.preload) into distros whose
 * glibc (2.39+) implements lchmod() and fchmodat(..., AT_SYMLINK_NOFOLLOW)
 * with the fchmodat2 syscall.
 *
 * Termux's proot (up to 5.1.107.95) doesn't know fchmodat2, so on Linux 6.6+
 * the call reaches the kernel with the guest path untranslated and fails
 * with ENOENT. libarchive then reports "Can't set permissions" for every
 * symlink it extracts, which xbps (Void) treats as fatal.
 *
 * Here, AT_SYMLINK_NOFOLLOW on a symlink fails with EOPNOTSUPP, as Linux
 * itself does (symlink modes can't be changed), and everything else uses
 * the old three-argument fchmodat syscall, which proot translates.
 * Upstream proot fixed this in proot-me/proot#408 (fchmodat2 support).
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

#ifndef SYS_fchmodat2
#define SYS_fchmodat2 452
#endif

int fchmodat(int dirfd, const char *path, mode_t mode, int flags)
{
	struct stat st;

	if (flags & ~AT_SYMLINK_NOFOLLOW)
		return syscall(SYS_fchmodat2, dirfd, path, mode, flags);
	if (flags & AT_SYMLINK_NOFOLLOW) {
		if (fstatat(dirfd, path, &st, AT_SYMLINK_NOFOLLOW) != 0)
			return -1;
		if (S_ISLNK(st.st_mode)) {
			errno = EOPNOTSUPP;
			return -1;
		}
	}
	return syscall(SYS_fchmodat, dirfd, path, mode);
}

int lchmod(const char *path, mode_t mode)
{
	return fchmodat(AT_FDCWD, path, mode, AT_SYMLINK_NOFOLLOW);
}
