/*
 * libandronix-shm.so: System V shared memory in files, for PostgreSQL.
 *
 * Android kernels have no SysV IPC; Termux's proot emulates it (--sysvipc)
 * with a helper process, and initdb deadlocks against that helper on
 * Android 17 (docs/packs.md). andronix-postgres preloads this library for
 * the postgres processes only, so their shmget/shmat/shmdt/shmctl never
 * reach proot.
 *
 * A segment is a file /tmp/.andronix-shm/<id>; the id is the key (or a
 * random one for IPC_PRIVATE). Attaching maps the file shared and holds a
 * shared flock on it, which forked children inherit and exec drops (the
 * descriptor is close-on-exec), like a real attachment. shm_nattch is 1
 * while any process holds one, which is what PostgreSQL's stale-segment
 * check (PGSharedMemoryAttach) looks at. IPC_RMID unlinks the file; live
 * mappings stay, as with the kernel's.
 *
 * Build: ci/build-preload.sh. Only libc calls whose symbols glibc and musl
 * share are used.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/ipc.h>
#include <sys/mman.h>
#include <sys/shm.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#define DIR "/tmp/.andronix-shm"
#define MAX_ATTACH 64

static struct {
    void *addr;
    size_t size;
    int fd;
} attached[MAX_ATTACH];

static void path_of(int id, char *buf, size_t n) { snprintf(buf, n, DIR "/%d", id); }

static void ensure_dir(void) {
    if (mkdir(DIR, 01777) == 0) chmod(DIR, 01777); /* umask-proof */
}

int shmget(key_t key, size_t size, int flags) {
    char p[64];
    ensure_dir();
    if (key == IPC_PRIVATE) {
        for (int tries = 0; tries < 64; tries++) {
            int id = 0x40000000 | ((int) ((unsigned) rand() ^ (unsigned) getpid() << 8 ^ (unsigned) time(NULL)) & 0x3fffffff);
            path_of(id, p, sizeof p);
            int fd = open(p, O_RDWR | O_CREAT | O_EXCL | O_CLOEXEC, (flags & 0777) | 0600);
            if (fd < 0) {
                if (errno == EEXIST) continue;
                return -1;
            }
            if (ftruncate(fd, (off_t) size) < 0) {
                int e = errno;
                close(fd);
                unlink(p);
                errno = e;
                return -1;
            }
            close(fd);
            return id;
        }
        errno = ENOSPC;
        return -1;
    }
    int id = (int) key & 0x7fffffff;
    if (id == 0) id = 1;
    path_of(id, p, sizeof p);
    int fd;
    if (flags & IPC_CREAT) {
        fd = open(p, O_RDWR | O_CREAT | O_EXCL | O_CLOEXEC, (flags & 0777) | 0600);
        if (fd >= 0) {
            if (ftruncate(fd, (off_t) size) < 0) {
                int e = errno;
                close(fd);
                unlink(p);
                errno = e;
                return -1;
            }
            close(fd);
            return id;
        }
        if (errno != EEXIST) return -1;
        if (flags & IPC_EXCL) return -1; /* errno is EEXIST */
    }
    fd = open(p, O_RDONLY | O_CLOEXEC);
    if (fd < 0) return -1; /* ENOENT, as the kernel */
    struct stat st;
    int r = fstat(fd, &st);
    close(fd);
    if (r < 0) return -1;
    if ((size_t) st.st_size < size) {
        errno = EINVAL;
        return -1;
    }
    return id;
}

void *shmat(int id, const void *addr, int flags) {
    char p[64];
    path_of(id, p, sizeof p);
    int ro = flags & SHM_RDONLY;
    int fd = open(p, (ro ? O_RDONLY : O_RDWR) | O_CLOEXEC);
    if (fd < 0) {
        errno = EINVAL;
        return (void *) -1;
    }
    struct stat st;
    if (fstat(fd, &st) < 0 || flock(fd, LOCK_SH) < 0) {
        close(fd);
        errno = EINVAL;
        return (void *) -1;
    }
    int slot = -1;
    for (int i = 0; i < MAX_ATTACH; i++)
        if (!attached[i].addr) {
            slot = i;
            break;
        }
    if (slot < 0) {
        close(fd);
        errno = EMFILE;
        return (void *) -1;
    }
    void *want = (void *) addr; /* PostgreSQL passes NULL; SHM_RND isn't supported */
    void *m = mmap(want, (size_t) st.st_size, ro ? PROT_READ : PROT_READ | PROT_WRITE,
                   MAP_SHARED | (want ? MAP_FIXED : 0), fd, 0);
    if (m == MAP_FAILED) {
        int e = errno;
        close(fd);
        errno = e;
        return (void *) -1;
    }
    attached[slot].addr = m;
    attached[slot].size = (size_t) st.st_size;
    attached[slot].fd = fd;
    return m;
}

int shmdt(const void *addr) {
    for (int i = 0; i < MAX_ATTACH; i++)
        if (attached[i].addr && attached[i].addr == addr) {
            munmap(attached[i].addr, attached[i].size);
            close(attached[i].fd);
            attached[i].addr = NULL;
            return 0;
        }
    errno = EINVAL;
    return -1;
}

int shmctl(int id, int cmd, struct shmid_ds *buf) {
    char p[64];
    path_of(id, p, sizeof p);
    switch (cmd) {
    case IPC_RMID:
        if (unlink(p) < 0) {
            errno = EINVAL;
            return -1;
        }
        return 0;
    case IPC_SET:
        return access(p, F_OK) == 0 ? 0 : (errno = EINVAL, -1);
    case IPC_STAT: {
        struct stat st;
        if (stat(p, &st) < 0) {
            errno = EINVAL;
            return -1;
        }
        if (!buf) {
            errno = EFAULT;
            return -1;
        }
        memset(buf, 0, sizeof *buf);
        buf->shm_perm.uid = buf->shm_perm.cuid = st.st_uid;
        buf->shm_perm.gid = buf->shm_perm.cgid = st.st_gid;
        buf->shm_perm.mode = st.st_mode & 0777;
        buf->shm_segsz = (size_t) st.st_size;
        buf->shm_ctime = st.st_ctime;
        /* Attached somewhere: someone holds the shared lock. */
        int fd = open(p, O_RDONLY | O_CLOEXEC);
        if (fd >= 0) {
            if (flock(fd, LOCK_EX | LOCK_NB) < 0 && errno == EWOULDBLOCK)
                buf->shm_nattch = 1;
            else
                flock(fd, LOCK_UN);
            close(fd);
        }
        return 0;
    }
    }
    errno = EINVAL;
    return -1;
}
