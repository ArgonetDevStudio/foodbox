package shanepark.foodbox.api.repository;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import shanepark.foodbox.api.config.ObjectMapperConfig;
import shanepark.foodbox.api.domain.Menu;

import java.io.File;
import java.io.IOException;
import java.time.LocalDate;
import java.util.List;
import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;

class MenuRepositoryTest {

    private MenuRepository menuRepository;

    @BeforeEach
    void setUp() throws IOException {
        File tmpFile = File.createTempFile("test-db", ".json");
        ObjectMapper objectMapper = new ObjectMapperConfig().mapper();
        menuRepository = new MenuRepository(tmpFile, objectMapper);
    }

    @Test
    void save_shouldStoreMenuResponse() {
        Menu menu = new Menu(LocalDate.now(), List.of("Breakfast", "Lunch", "Dinner"));

        menuRepository.saveAll(List.of(menu));
        Optional<Menu> retrieved = menuRepository.findByDate(menu.getDate());

        assertThat(retrieved).isPresent();
        assertThat(retrieved.get()).isEqualTo(menu);
        assertThat(retrieved.get().getMenus()).containsExactly("Breakfast", "Lunch", "Dinner");
    }

    @Test
    void findByDate_shouldReturnEmptyOptionalIfDateNotFound() {
        Optional<Menu> retrieved = menuRepository.findByDate(LocalDate.now());

        assertThat(retrieved).isEmpty();
    }

    @Test
    void findAll_shouldReturnAllSavedMenus() {
        Menu menu1 = new Menu(LocalDate.now(), List.of("Breakfast", "Lunch"));
        Menu menu2 = new Menu(LocalDate.now().plusDays(1), List.of("Brunch", "Supper"));
        menuRepository.saveAll(List.of(menu1, menu2));

        List<Menu> allMenus = menuRepository.findAll();

        assertThat(allMenus).containsExactlyInAnyOrder(menu1, menu2);
    }

    @Test
    void findAll_shouldReturnEmptyListIfNoMenusSaved() {
        List<Menu> allMenus = menuRepository.findAll();

        assertThat(allMenus).isEmpty();
    }
}

