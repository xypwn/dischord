# Dischord
### A simple, easy-to-deploy Discord music bot written in go
### Supports YouTube, Spotify and hundreds of other sites using youtube-dl

---

## Setup
- [Windows](#windows)
- [MacOS](#macos)
- [Linux](#linux)
- [From source](#from-source)

### Windows
#### Download .exe from releases
- [64-bit](https://github.com/xypwn/Dischord/releases/download/latest/dischord-windows-amd64.exe) (reasonably modern hardware)
- [32-bit](https://github.com/xypwn/Dischord/releases/download/latest/dischord-windows-x86.exe) (very old hardware)

#### Preparations
After you're done downloading, I would recommend putting the executable into a new
folder so your downloads don't get too cluttered, since a few more files will
be created upon running the executable.

#### Initial setup / running the .exe
To start the program, simply double click the .exe. This will bring up a command
window saying it will have to first download a few additional programs. After
it's finished downloading, edit the newly generated config.toml file and
replace the text after `token = ` with your Discord bot token, leaving the
surrounding "". After editing, the line should look something like this:

```toml
token = "dmMpDY4K8dFyqMoypaZg3QXP.QUp5Sg.e7VQhRpEfud6SajSvyFZpxZpVwwBrwNYr2L3m7"
```

Now if you start the .exe again, it should just work.

### MacOS
#### Download the executable from releases
- [Apple silicon](https://github.com/xypwn/Dischord/releases/download/latest/dischord-macos-apple-silicon) (newer models)
- [Intel hardware (untested)](https://github.com/xypwn/Dischord/releases/download/latest/dischord-macos-intel) (slightly older models)

#### Preparations
After you're done downloading, I would recommend putting the executable into a new
folder so your downloads don't get too cluttered, since a few more files will
be created upon running the executable.

In order to run the program, you will need to enable opening a terminal in
the current folder. To do so, go to
**System Preferences -> Keyboard -> Keyboard Shortcuts -> Services**
and enable
**Files and Folders -> New Terminal at Folder** and **New Terminal Tab at Folder**.

#### Initial setup / running the executable
Navigate to the folder containing your executable, right-click and select
**Services -> New Terminal at Folder**. In the command
window that opens up, type `chmod +x dischord-macos-*` and hit `Enter` (you
will only need to do this once).

Then, to run the executable, type `./dischord-macos-*` and hit `Enter`.

On the first run, it will download a few additional programs.
When it's done, open the newly generated config.toml file with a text editor
and replace the text after `token = ` with your Discord bot token, leaving the
surrounding "". After editing, the line should look something like this:

```toml
token = "dmMpDY4K8dFyqMoypaZg3QXP.QUp5Sg.e7VQhRpEfud6SajSvyFZpxZpVwwBrwNYr2L3m7"
```

Done! Now you can just run the executable and everything should work.

### Linux
#### Download the executable from releases
- [amd64/x86_64/x64](https://github.com/xypwn/Dischord/releases/download/latest/dischord-linux-amd64)
- [i386/x86](https://github.com/xypwn/Dischord/releases/download/latest/dischord-linux-x86)
- [arm64 (untested)](https://github.com/xypwn/Dischord/releases/download/latest/dischord-linux-arm64)
- [armhf/arm32 (untested)](https://github.com/xypwn/Dischord/releases/download/latest/dischord-linux-arm32)

#### Preparations
After you're done downloading, I would recommend putting the executable into a new
folder so your downloads don't get too cluttered, since a few more files will
be created upon running the executable.

#### Initial setup / running the executable
First, `cd` into the executable's directory.

Then, run `chmod +x dischord-linux-*` to make the file executable.

Run the executable with `./dischord-linux-*`.

On the first run, it will download **youtube-dl** and **FFmpeg** if they aren't
already installed on your system (for example through your package manager).
When it's done, open the newly generated config.toml file with a text editor
and replace the text after `token = ` with your Discord bot token, leaving the
surrounding "". After editing, the line should look something like this:

```toml
token = "dmMpDY4K8dFyqMoypaZg3QXP.QUp5Sg.e7VQhRpEfud6SajSvyFZpxZpVwwBrwNYr2L3m7"
```

Done! Now you can just run the executable and everything should work.

### From source

After installing [go](https://go.dev/dl/), you can simply run the makefile to
build a native binary.

In case you are using a non-Linux OS, you will have to manually install
[youtube-dl](https://yt-dl.org/) and [FFmpeg](https://ffmpeg.org/) first before being able to run the bot.
