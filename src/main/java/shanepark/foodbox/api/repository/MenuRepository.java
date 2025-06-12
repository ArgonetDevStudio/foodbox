package shanepark.foodbox.api.repository;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.annotation.PostConstruct;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.stereotype.Repository;
import shanepark.foodbox.api.domain.Menu;

import java.io.File;
import java.io.IOException;
import java.time.LocalDate;
import java.util.*;

@Repository
@Slf4j
public class MenuRepository {
    private final ObjectMapper objectMapper;
    private final File dbFile;

    @Autowired
    public MenuRepository(@Qualifier("dbFile") File dbFile, ObjectMapper mapper) {
        this.dbFile = dbFile;
        this.objectMapper = mapper;
    }

    @PostConstruct
    public Map<LocalDate, Menu> loadFromFile() {
        Map<LocalDate, Menu> menuMap = new HashMap<>();
        if (dbFile.length() == 0) {
            return menuMap;
        }
        try {
            List<Menu> loaded = objectMapper.readValue(dbFile, new TypeReference<>() {
            });
            for (Menu menu : loaded) {
                menuMap.put(menu.getDate(), menu);
            }
        } catch (IOException e) {
            log.error("Failed to create menu data file", e);
            System.exit(1);
        }
        return menuMap;
    }

    public void flush(Map<LocalDate, Menu> menuMap) {
        File tempFile = new File(dbFile.getAbsolutePath() + ".tmp");
        try {
            List<Menu> saveTarget = menuMap.values()
                    .stream()
                    .sorted()
                    .toList();
            objectMapper.writerWithDefaultPrettyPrinter().writeValue(tempFile, saveTarget);
            if (!tempFile.renameTo(dbFile)) {
                throw new IOException("Failed to rename temp file to db file");
            }
        } catch (IOException e) {
            log.warn("Failed to save menu data", e);
        }
    }

    public Optional<Menu> findByDate(LocalDate date) {
        return Optional.ofNullable(loadFromFile().get(date));
    }

    public List<Menu> findAll() {
        return loadFromFile().values().stream()
                .sorted(Comparator.comparingLong(m -> m.getDate().toEpochDay()))
                .toList().reversed();
    }

    public void saveAll(List<Menu> menus) {
        Map<LocalDate, Menu> menuMap = loadFromFile();
        for (Menu menu : menus) {
            menuMap.put(menu.getDate(), menu);
        }
        flush(menuMap);
    }
}
