# GoogleTakeoutFixer

A tool that will soon allow you to easily merge Google's weird JSON metadata with your images.

> [!IMPORTANT]
> This project is still in early development. Not ready for use yet.

## The Issue
When you download your images from Google's "Google Photos" service through "Google Takeout", the metadata (location, time of creation etc.) are not saved within your files, but instead saved in JSON files.\
This causes issues when:
- Chronologically sorting your images
- Organizing photos by date/location

## Solution
GoogleTakeoutFixer automatically reads the JSON files and writes the metadata back to your image/video files where it belongs.

## Planned features
- A simple, user friendly GUI
- Language support for non english takeouts (only "Photos from (year)" works currently)
- Progress tracking
