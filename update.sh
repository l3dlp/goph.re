cd /intelligences/apps/goph.re
bin/gophre update

# Sauvegarde quotidienne de la base SQLite, 7 copies glissantes (backup/gophre-<jour>.db)
if [ "$(date +%H)" = "04" ]; then
	mkdir -p backup
	rm -f backup/gophre.db.tmp
	bin/gophre backup backup/gophre.db.tmp && mv backup/gophre.db.tmp "backup/gophre-$(date +%u).db"
fi

gigit "Hourly update"

# Repack quand les objets loose s'accumulent (seuil gc.auto=200 dans .git/config)
git gc --auto --quiet
