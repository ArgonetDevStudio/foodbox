package shanepark.foodbox.api.repository;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import jakarta.annotation.PostConstruct;
import jakarta.annotation.PreDestroy;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.stereotype.Repository;
import shanepark.foodbox.api.domain.Menu;

import java.io.File;
import java.io.IOException;
import java.time.LocalDate;
import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;

@Repository
@Slf4j
public class MenuRepository {
    private final ConcurrentHashMap<LocalDate, Menu> menuMap = new ConcurrentHashMap<>();
    private final ObjectMapper objectMapper;
    private final File dbFile;

    @Autowired
    public MenuRepository(@Qualifier("dbFile") File dbFile, ObjectMapper mapper) {
        this.dbFile = dbFile;
        this.objectMapper = mapper;
    }

    @PostConstruct
    public void loadFromFile() {
        if (dbFile.length() == 0) {
            return;
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
    }

    @PreDestroy
    public void flush() {
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
        return Optional.ofNullable(menuMap.get(date));
    }

    public List<Menu> findAll() {
        return menuMap.values().stream()
                .sorted(Comparator.comparingLong(m -> m.getDate().toEpochDay()))
                .toList().reversed();
    }

    public void saveAll(List<Menu> menus) {
        for (Menu menu : menus) {
            menuMap.put(menu.getDate(), menu);
        }
        flush();
    }
}
