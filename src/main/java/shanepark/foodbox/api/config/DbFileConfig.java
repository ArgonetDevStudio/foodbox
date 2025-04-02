package shanepark.foodbox.api.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.io.File;
import java.io.IOException;

@Configuration
@Slf4j
public class DbFileConfig {

    @Value("${foodbox.db-file-dir}")
    private String dbFileDir;

    @Bean(name = "dbFile")
    public File storageFile() {
        File dir = new File(this.dbFileDir);
        File dbFile = new File(dir, "db.json");
        if (!dbFile.exists()) {
            if (!dir.mkdirs()) {
                log.warn("Failed to create menu data directory");
            }
            try {
                if (!dbFile.createNewFile()) {
                    log.warn("Failed to create menu data file");
                }
            } catch (IOException e) {
                log.warn("Failed to create menu data file", e);
                throw new RuntimeException(e);
            }
        }
        log.info("dbFile registered: {}", dbFile.getAbsolutePath());
        return dbFile;
    }

}
