#!/bin/sh
# Frames the Flutter screenshot goldens as phones for the README.
# sh docs/images/screenshots/phones.sh   (from the repo root; needs ImageMagick)
set -e
G=mobile/test/screenshots/goldens
O=docs/images/screenshots/app
frame() { # <golden> <out-name>
  magick "$G/$1" -resize 380x844 \
    \( +clone -alpha extract -fill black -colorize 100 -fill white -draw "roundrectangle 0,0 379,843 34,34" \) \
    -alpha off -compose CopyOpacity -composite \
    \( -size 404x868 xc:none -fill '#1b1d22' -draw "roundrectangle 0,0 403,867 46,46" \
       -fill none -stroke '#3a3d45' -strokewidth 2 -draw "roundrectangle 1,1 402,866 45,45" \) \
    +swap -gravity center -compose over -composite \
    -fill '#1b1d22' -draw "circle 202,24 202,30" \
    -quality 88 -define webp:method=6 "$O/$2.webp"
}
frame directions/rack/home_light_412x915.png              home-rack-light
frame directions/rack/files_light_412x915.png             files-rack-light
frame apps/app_store_light_412x915.png                    app-store-light
frame backup/rack/backup_overview_light_412x915.png       backup-rack-light
frame directions/tonal/home_black_412x915.png             home-tonal-black
frame directions/console/health_black_412x915.png         health-console-black
frame fans/console/fans_overview_black_412x915.png        fans-console-black
frame download_station/tonal/ds_list_black_412x915.png    downloads-tonal-black
