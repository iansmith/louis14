 Create the fonts directory
mkdir -p ~/louis14/fonts

# Unzip your merged font file
cd ~/Downloads
unzip Atkinson_Hyperlegible.zip -d /tmp/atkinson_temp

# Copy all TTF files to your project
find /tmp/atkinson_temp -name "*.ttf" -exec cp {} ~/louis14/fonts/ \;

# Check what you got
ls -la ~/louis14/fonts/

# Cleanup temp files
rm -rf /tmp/atkinson_temp

# Optionally install to system fonts (recommended for easier access)
sudo cp ~/louis14/fonts/*.ttf /Library/Fonts/
