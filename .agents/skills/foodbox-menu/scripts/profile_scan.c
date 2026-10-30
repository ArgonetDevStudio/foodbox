/* Read-only helper for a local Wine desktop. See README.md. */
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* This host has Wine import libraries but no Windows SDK headers. */
#define WINAPI __attribute__((ms_abi))
typedef uint32_t DWORD;
typedef uint16_t WCHAR;
typedef int BOOL;
typedef void *HANDLE;
typedef void *HWND;
typedef intptr_t LPARAM;
typedef struct {
    void *BaseAddress, *AllocationBase;
    DWORD AllocationProtect;
    uint16_t PartitionId;
    size_t RegionSize;
    DWORD State, Protect, Type;
} MEMORY_BASIC_INFORMATION;
extern BOOL WINAPI EnumWindows(BOOL (WINAPI *)(HWND, LPARAM), LPARAM);
extern int WINAPI GetWindowTextW(HWND, WCHAR *, int);
extern int WINAPI MultiByteToWideChar(unsigned, DWORD, const char *, int, WCHAR *, int);
extern DWORD WINAPI GetWindowThreadProcessId(HWND, DWORD *);
extern HANDLE WINAPI OpenProcess(DWORD, BOOL, DWORD);
extern DWORD WINAPI GetLastError(void);
extern size_t WINAPI VirtualQueryEx(HANDLE, const void *, MEMORY_BASIC_INFORMATION *, size_t);
extern BOOL WINAPI ReadProcessMemory(HANDLE, const void *, void *, size_t, size_t *);
extern BOOL WINAPI CloseHandle(HANDLE);

static DWORD target;
static BOOL multiple;
static WCHAR expected[257];
static BOOL WINAPI find_window(HWND window, LPARAM unused) {
    (void)unused;
    WCHAR title[257] = {0};
    GetWindowTextW(window, title, 257);
    size_t length = 0;
    while (expected[length]) length++;
    if (!memcmp(title, expected, (length + 1) * sizeof(WCHAR))) {
        DWORD pid;
        GetWindowThreadProcessId(window, &pid);
        if (target && target != pid) multiple = 1;
        target = pid;
    }
    return 1;
}

int main(int argc, char **argv) {
    if (argc != 3 || !argv[1][0] || strlen(argv[1]) > 512 || strlen(argv[1]) % 2 ||
        !argv[2][0] || strlen(argv[2]) > 512 || strlen(argv[2]) % 2) {
        fputs("Expected hex-encoded UTF-8 nickname and window title.\n", stderr);
        return 2;
    }
    unsigned char needle[256];
    size_t needle_size = strlen(argv[1]) / 2;
    for (size_t i = 0; i < needle_size; i++) {
        unsigned value;
        if (sscanf(argv[1] + 2 * i, "%2x", &value) != 1) return 2;
        needle[i] = (unsigned char)value;
    }
    char window_title[256];
    size_t title_size = strlen(argv[2]) / 2;
    for (size_t i = 0; i < title_size; i++) {
        unsigned value;
        if (sscanf(argv[2] + 2 * i, "%2x", &value) != 1) return 2;
        window_title[i] = (char)value;
    }
    int title_length = MultiByteToWideChar(65001, 0, window_title, (int)title_size, expected, 256);
    if (title_length <= 0) return 2;
    expected[title_length] = 0;
    EnumWindows(find_window, 0);
    if (!target || multiple) {
        fputs("Expected one process with the requested window title in this Wine prefix.\n", stderr);
        return 3;
    }
    HANDLE process = OpenProcess(0x400 | 0x10, 0, target);
    if (!process) {
        fprintf(stderr, "Read-only OpenProcess failed (Windows error %u).\n", GetLastError());
        return 4;
    }
    uintptr_t address = 0;
    MEMORY_BASIC_INFORMATION region;
    size_t scanned = 0;
    unsigned hits = 0;
    while (VirtualQueryEx(process, (void *)address, &region, sizeof(region)) == sizeof(region)) {
        uintptr_t base = (uintptr_t)region.BaseAddress;
        size_t size = region.RegionSize;
        if (!size || base + size <= address) break;
        if (region.State == 0x1000 && !(region.Protect & (1 | 0x100)) && size <= 128 * 1024 * 1024) {
            unsigned char *buffer = malloc(size);
            size_t got = 0;
            if (!buffer) { CloseHandle(process); return 5; }
            if (ReadProcessMemory(process, (void *)base, buffer, size, &got)) {
                scanned += got;
                for (size_t i = 0; i + needle_size <= got; i++) {
                    if (memcmp(buffer + i, needle, needle_size)) continue;
                    if (++hits > 128) {
                        free(buffer); CloseHandle(process);
                        fputs("Too many nickname matches; close other profile viewers and retry.\n", stderr);
                        return 6;
                    }
                    size_t left = i < 16384 ? i : 16384;
                    size_t right = got - i < 16384 ? got - i : 16384;
                    uint32_t length = (uint32_t)(left + right);
                    if (fwrite(&length, sizeof(length), 1, stdout) != 1 ||
                        fwrite(buffer + i - left, 1, length, stdout) != length) {
                        free(buffer); CloseHandle(process); return 7;
                    }
                }
            }
            free(buffer);
        }
        address = base + size;
    }
    CloseHandle(process);
    fprintf(stderr, "Read-only scan: %u nickname matches, %zu bytes inspected.\n", hits, scanned);
    return 0;
}
