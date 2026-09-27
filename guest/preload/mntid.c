/*
 * libandronix-mntid.so: preloaded (/etc/ld.so.preload) where statx() can't
 * report a mount id and name_to_handle_at() isn't there either: kernels
 * before 5.8 under proot (the compat probe: statx_mnt_id missing).
 *
 * systemd 256+ resolves every path with chase(), which needs mount ids.
 * Without them it fails with EUNATCH ("Protocol driver not attached"):
 * systemd-sysusers and systemd-tmpfiles can't read their config ("Failed to
 * read 'basic.conf'"), so sudo, dbus and a whole desktop stay unconfigured
 * (Kali, Redmi Note 7 Pro on 4.14; andronix-a9 on 4.4).
 *
 * When a caller asks statx() for STATX_MNT_ID(_UNIQUE) and the kernel
 * didn't provide it, this fills it from the device number: one id per
 * filesystem, which is what chase() compares. Nothing else changes.
 */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdint.h>
#include <sys/stat.h>
#include <sys/syscall.h>

#ifndef STATX_MNT_ID
#define STATX_MNT_ID 0x1000U
#endif
#ifndef STATX_MNT_ID_UNIQUE
#define STATX_MNT_ID_UNIQUE 0x4000U
#endif
#ifndef SYS_statx
#define SYS_statx 291
#endif

static void fill(unsigned int mask, struct statx *st)
{
	unsigned int want = mask & (STATX_MNT_ID | STATX_MNT_ID_UNIQUE);
	if (!want || (st->stx_mask & (STATX_MNT_ID | STATX_MNT_ID_UNIQUE)))
		return;
	st->stx_mnt_id = ((uint64_t)st->stx_dev_major << 32 | st->stx_dev_minor) + 1;
	st->stx_mask |= want;
}

int statx(int dirfd, const char *path, int flags, unsigned int mask, struct statx *st)
{
	static int (*real)(int, const char *, int, unsigned int, struct statx *);
	if (!real)
		real = (int (*)(int, const char *, int, unsigned int, struct statx *))dlsym(RTLD_NEXT, "statx");
	int r = real(dirfd, path, flags, mask, st);
	if (r == 0)
		fill(mask, st);
	return r;
}

/* Programs calling the statx syscall directly through syscall(). */
long syscall(long number, ...)
{
	static long (*real)(long, ...);
	va_list ap;
	long a[6];

	va_start(ap, number);
	for (int i = 0; i < 6; i++)
		a[i] = va_arg(ap, long);
	va_end(ap);
	if (!real)
		real = (long (*)(long, ...))dlsym(RTLD_NEXT, "syscall");
	long r = real(number, a[0], a[1], a[2], a[3], a[4], a[5]);
	if (number == SYS_statx && r == 0)
		fill((unsigned int)a[3], (struct statx *)a[4]);
	return r;
}
