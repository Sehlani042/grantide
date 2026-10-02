# Recover the verified Chrome path

Read if supported Chrome controls fail or the native bridge is missing.

- Multiple Chrome processes can share a bundle name. Select the observed persistent Grantide profile, running without `--disable-extensions`, rather than the first Chrome process.
- Reuse the installed extension/native bridge. A custom `--user-data-dir` needs its native manifest under that root's `NativeMessagingHosts`. The installer accepts `--chrome-user-data-dir ROOT`; read [setup](../../../docs/CHROME-EXTENSION.md). Installation/new access follows current tool confirmation requirements.
- A native-control timeout is not authentication failure. Inspect available controls. AppleScript is an alternative only when explicitly authorized in the calling task.
- The verified macOS AppleScript recovery resolved the dedicated Chrome PID to a numeric `System Events` process index; name-based references selected another headless Chrome. Indices change: resolve fresh before use. Bring the intended process frontmost before keyboard input and inspect fresh AX state.
- Select fields using observed labels/placeholders. During an authorized CAPTCHA, read only its challenge field; never query password/MFA values. Pause observations for human credential/MFA entry.
- A Chrome window may be an autofill, translate, password-save or omnibox popup. Confirm title and UI before clicking. Dismiss an unrequested password-save prompt without saving.
- IAB and the dedicated Chrome have separate cookies. Do not copy cookies between them; inspect the actual authenticated profile.

These are observations from the 2026-10-02 verified login, not authority to bypass warnings or inspect unrelated windows.
