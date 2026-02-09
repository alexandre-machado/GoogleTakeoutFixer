# GoogleTakeoutFixer

<p align="center">
    <img src="images/GoogleTakeoutFixer.png" alt="drawing" width="200"/>
</p>

A tool that allows you to easily merge Google's weird JSON metadata with your images.

## The Issue
When you download your files from Google's "Google Photos" service through "Google Takeout", the exported data is **inconsistently organized and often fragmented.**
This can lead to problems:
- Files cannot be reliably sorted or grouped by date or location
- The export contains unnecessary files and a cluttered folder structure
- Your takeout having a big file size due to duplicated media and unnecessary JSON files

## Solution
GoogleTakeoutFixer solves these issues by:
- **Writing EXIF metadata** directly into your media.
- **Organizing your files** into a clear and structured folder structure for easier navigation.
- **Automatically removing unnecessary JSON files**.

## Preview
<p align="center">
    <img src="images/GTFWindow.png" alt="GoogleTakeoutFixer Window" width="400"/>
</p>

## Tutorial
### 1. Preparation
To use GoogleTakeoutFixer, you must have downloaded your photos from Google Takeout and extracted them. Follow these steps:

1. Go to [Google Takeout](https://takeout.google.com/) and click "Deselect all".

    <img src="images/DeselectAllTakeout.png" alt="Google Takeout deselect button" width="400"/>
2. Scroll down and select "Google Photos".

    <img src="images/TakeoutPhotosSelect.png" alt="Google Takeout Selected" width="400"/>
3. Scroll down to the bottom and click "Next Step".

4. In the "Transfer to" section, choose how you'd like to receive your download link. I recommend choosing email. For "File size", select 50 GB for easier handling.

    <img src="images/CreateExportTakeout.png" alt="Create Export options" width=300>
5. Click "Create export" and follow the instructions.

> [!NOTE]
> - If your Google Takeout exceeds the 50 GB limit and is split into multiple archives, extract all the archives and move the extracted files into a single folder. This ensures that GoogleTakeoutFixer can process all your files correctly.
> - Select the folder named "Google Photos" as your input folder. This folder should contain subfolders like "Photos from (year)" and folders with the names of your albums. Do not select a parent folder of "Google Photos".

### 2. Installation
1. Download the latest release of GoogleTakeoutFixer from the [release page](https://github.com/feloex/GoogleTakeoutFixer/releases). Choose the version that matches your operating system.
2. Extract the downloaded archive.
3. Run the executable file.

### 3. Using GoogleTakeoutFixer
1. Click **"Select Google Takeout folder"** and choose the folder where you extracted your Google Takeout photos. This folder is named something like "Google Photos".
2. Click **"Select output folder"** and choose the folder where you want the fixed photos to be saved.
3. Choose the options that you want to apply:
    - **"Write EXIF metadata"**: Writes metadata from JSON files into the media files. May not be necessary.
    - **"Use symlinks for albums"**: Creates file links instead of duplicating files for albums.
5. Click **"Start processing"** and wait for the process to finish. The time it takes depends on the number of photos and videos you have.

Once the process is complete, you can find your fixed files in the output folder you selected.

## Planned Features
- Support for more languages
- Cancel button during processing in the GUI
- Better looking and more user-friendly GUI
- More options for how to organize / structure the output folder

## Credits
This project modifies metadata using the [ExifTool](https://exiftool.org/) library by **Phil Harvey**. ExifTool is licensed under the Perl Artistic license, or the GNU General Public License (see [here](https://exiftool.org/#license) for more details).

## Disclaimer
This project is an **independent open-source project** and is **not affiliated with, endorsed, or sponsored by Google LLC or any of its subsidiaries**. The use of the name "Google" in this repository is solely for descriptive purposes.
