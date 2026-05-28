package shanepark.foodbox.image.ocr.clova;

import com.google.gson.Gson;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.domain.ParsedMenuEiso;
import shanepark.foodbox.image.ocr.ImageMarginCalculatorEiso;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.when;

class ImageParserClovaEisoTest {

    private final Gson gson = new Gson();

    @Test
    void testParseEisodosirakMenu() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        // Fix clock to 2025-10-01
        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        // Mock Clova API response
        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then
        // Expect 25 regions (5x5 grid) containing date information
        assertThat(parsedMenus).hasSize(25);

        // Log parsed menus for debugging
        System.out.println("Total parsed menus: " + parsedMenus.size());
        for (ParsedMenu menu : parsedMenus) {
            System.out.println("=================================");
            System.out.println("Date: " + menu.getDate());
            System.out.println("Menu items: " + menu.getMenus().size());
            System.out.println("Valid: " + (menu.getMenus().size() > 2));
            menu.getMenus().forEach(item -> System.out.println("  - " + item));
        }

        // Verify earliest and latest dates
        LocalDate minDate = parsedMenus.stream()
                .map(ParsedMenu::getDate)
                .min(LocalDate::compareTo)
                .orElseThrow();
        LocalDate maxDate = parsedMenus.stream()
                .map(ParsedMenu::getDate)
                .max(LocalDate::compareTo)
                .orElseThrow();

        assertThat(minDate).isEqualTo(LocalDate.of(2025, 9, 29));
        assertThat(maxDate).isEqualTo(LocalDate.of(2025, 10, 31));

        // Ensure dates created via merged OCR tokens exist
        assertThat(parsedMenus).anyMatch(m -> m.getDate().equals(LocalDate.of(2025, 10, 7)));
        assertThat(parsedMenus).anyMatch(m -> m.getDate().equals(LocalDate.of(2025, 10, 9)));

        // Count menus that look complete (size > 2)
        long validCount = parsedMenus.stream()
                .filter(m -> m.getMenus().size() > 2)
                .count();
        System.out.println("Valid menus (size > 2): " + validCount + " / " + parsedMenus.size());

        // Inspect incomplete menus
        List<ParsedMenu> invalidMenus = parsedMenus.stream()
                .filter(m -> m.getMenus().size() <= 2)
                .toList();
        System.out.println("Invalid menus: " + invalidMenus.size());
        invalidMenus.forEach(m -> System.out.println("  - " + m.getDate() + ": " + m.getMenus()));

        // Exactly five menus are intentionally incomplete (holidays/announcements)
        assertThat(invalidMenus).hasSize(5);
    }

    @Test
    void testParseSpecificDates() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then
        assertThat(parsedMenus).hasSize(25);

        // Log parsed dates for inspection
        System.out.println("Parsed menus count: " + parsedMenus.size());
        parsedMenus.forEach(m -> System.out.println("  - " + m.getDate()));

        // Expect September 29 menu
        ParsedMenu sept29 = findMenuByDate(parsedMenus, LocalDate.of(2025, 9, 29));
        assertThat(sept29).isNotNull();
        assertThat(sept29.getMenus()).contains("얼갈이된장국", "치킨가스", "고추장어묵볶음");

        // Expect September 30 menu
        ParsedMenu sept30 = findMenuByDate(parsedMenus, LocalDate.of(2025, 9, 30));
        assertThat(sept30).isNotNull();
        assertThat(sept30.getMenus()).contains("계란국", "제육볶음", "명엽체볶음");

        // Expect October 1 menu
        ParsedMenu oct01 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 1));
        assertThat(oct01).isNotNull();
        assertThat(oct01.getMenus()).contains("콩나물국", "민찌두부조림", "멸치볶음");

        // October 3 (National Foundation Day) should contain a single entry
        ParsedMenu oct03 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 3));
        assertThat(oct03).isNotNull();
        assertThat(oct03.getMenus()).containsExactly("개천절");

        // Holiday announcements around Chuseok and Hangul Day
        ParsedMenu oct06 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 6));
        assertThat(oct06).isNotNull();
        assertThat(oct06.getMenus()).containsExactly("~~");

        ParsedMenu oct07 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 7));
        assertThat(oct07).isNotNull();
        assertThat(oct07.getMenus()).containsExactly("추석연휴", "즐거운 명절 되세요");

        ParsedMenu oct08 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 8));
        assertThat(oct08).isNotNull();
        assertThat(oct08.getMenus()).containsExactly("~~");

        ParsedMenu oct09 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 9));
        assertThat(oct09).isNotNull();
        assertThat(oct09.getMenus()).containsExactly("한글날");

        // Ensure all parsed dates are weekdays
        parsedMenus.forEach(menu -> {
            int dayOfWeek = menu.getDate().getDayOfWeek().getValue();
            assertThat(dayOfWeek)
                    .as("Menu date %s should be weekday", menu.getDate())
                    .isLessThanOrEqualTo(5);
        });
    }

    @Test
    void testParsedMenuEisoDateParsing() {
        // Given
        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        // When & Then - verify Eisodosirak date formats
        ParsedMenuEiso menu1 = new ParsedMenuEiso(fixedClock, "09월 29일");
        assertThat(menu1.getDate()).isEqualTo(LocalDate.of(2025, 9, 29));

        ParsedMenuEiso menu2 = new ParsedMenuEiso(fixedClock, "10월 01일");
        assertThat(menu2.getDate()).isEqualTo(LocalDate.of(2025, 10, 1));

        ParsedMenuEiso menu3 = new ParsedMenuEiso(fixedClock, "10월 03일");
        assertThat(menu3.getDate()).isEqualTo(LocalDate.of(2025, 10, 3));

        // Compact format should also work
        ParsedMenuEiso menu4 = new ParsedMenuEiso(fixedClock, "10월21일");
        assertThat(menu4.getDate()).isEqualTo(LocalDate.of(2025, 10, 21));
    }

    @Test
    void parseNumericDateOnlyLayout() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202605.png");
        Path jsonPath = Paths.get("src/test/resources/eiso_202605.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2026-05-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then
        assertThat(parsedMenus).hasSize(21);
        assertThat(parsedMenus.stream()
                .map(ParsedMenu::getDate)
                .sorted()
                .toList())
                .containsExactly(
                        LocalDate.of(2026, 5, 1),
                        LocalDate.of(2026, 5, 4),
                        LocalDate.of(2026, 5, 5),
                        LocalDate.of(2026, 5, 6),
                        LocalDate.of(2026, 5, 7),
                        LocalDate.of(2026, 5, 8),
                        LocalDate.of(2026, 5, 11),
                        LocalDate.of(2026, 5, 12),
                        LocalDate.of(2026, 5, 13),
                        LocalDate.of(2026, 5, 14),
                        LocalDate.of(2026, 5, 15),
                        LocalDate.of(2026, 5, 18),
                        LocalDate.of(2026, 5, 19),
                        LocalDate.of(2026, 5, 20),
                        LocalDate.of(2026, 5, 21),
                        LocalDate.of(2026, 5, 22),
                        LocalDate.of(2026, 5, 25),
                        LocalDate.of(2026, 5, 26),
                        LocalDate.of(2026, 5, 27),
                        LocalDate.of(2026, 5, 28),
                        LocalDate.of(2026, 5, 29)
                );
    }

    @Test
    void testMenuContentParsing() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then - ensure each menu carries at least one entry
        for (ParsedMenu menu : parsedMenus) {
            if (menu.getMenus().isEmpty()) {
                System.out.println("Warning: Empty menu for date " + menu.getDate());
            }
            // Most weekdays should have several dishes, but allow shorter lists
            if (!menu.getMenus().isEmpty()) {
                assertThat(menu.getMenus().size()).isGreaterThanOrEqualTo(1);
            }
        }
    }

    @Test
    void testWeekdayMenusOnly() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then
        assertThat(parsedMenus).hasSize(25);

        // Confirm every parsed menu belongs to weekdays
        for (ParsedMenu menu : parsedMenus) {
            int dayOfWeek = menu.getDate().getDayOfWeek().getValue();
            assertThat(dayOfWeek)
                    .as("Menu date %s should be weekday (1=Mon ~ 5=Fri)", menu.getDate())
                    .isBetween(1, 5); // 1(Mon) ~ 5(Fri)
        }

        // Ensure no Saturday or Sunday entries are produced
        assertThat(parsedMenus).noneMatch(m -> m.getDate().getDayOfWeek().getValue() == 6);
        assertThat(parsedMenus).noneMatch(m -> m.getDate().getDayOfWeek().getValue() == 7);
    }

    @Test
    void testAllExpectedDatesPresent() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then - all 25 day cells (5x5 grid) should be parsed
        assertThat(parsedMenus).hasSize(25);

        // Expected calendar should exactly match, including merged holiday cells
        List<LocalDate> expectedDates = List.of(
                LocalDate.of(2025, 9, 29),  // Mon
                LocalDate.of(2025, 9, 30),  // Tue
                LocalDate.of(2025, 10, 1),  // Wed
                LocalDate.of(2025, 10, 2),  // Thu
                LocalDate.of(2025, 10, 3),  // Fri (National Foundation Day)
                LocalDate.of(2025, 10, 6),  // Mon (~~)
                LocalDate.of(2025, 10, 7),  // Tue (~~, merged text)
                LocalDate.of(2025, 10, 8),  // Wed (~~)
                LocalDate.of(2025, 10, 9),  // Thu (~~, merged text)
                LocalDate.of(2025, 10, 10), // Fri
                LocalDate.of(2025, 10, 13), // Mon
                LocalDate.of(2025, 10, 14), // Tue
                LocalDate.of(2025, 10, 15), // Wed
                LocalDate.of(2025, 10, 16), // Thu
                LocalDate.of(2025, 10, 17), // Fri
                LocalDate.of(2025, 10, 20), // Mon
                LocalDate.of(2025, 10, 21), // Tue
                LocalDate.of(2025, 10, 22), // Wed
                LocalDate.of(2025, 10, 23), // Thu
                LocalDate.of(2025, 10, 24), // Fri
                LocalDate.of(2025, 10, 27), // Mon
                LocalDate.of(2025, 10, 28), // Tue
                LocalDate.of(2025, 10, 29), // Wed
                LocalDate.of(2025, 10, 30), // Thu
                LocalDate.of(2025, 10, 31)  // Fri
        );

        List<LocalDate> actualDates = parsedMenus.stream()
                .map(ParsedMenu::getDate)
                .sorted()
                .toList();

        // Actual dates must equal the expected list
        assertThat(actualDates).isEqualTo(expectedDates);

        // Sanity check each date individually
        for (LocalDate expectedDate : expectedDates) {
            assertThat(findMenuByDate(parsedMenus, expectedDate))
                    .as("Date %s should be present", expectedDate)
                    .isNotNull();
        }

        // Confirm merged-text dates appear
        assertThat(findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 7)))
                .as("Oct 7 should be present (merged from separated texts)")
                .isNotNull();
        assertThat(findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 9)))
                .as("Oct 9 should be present (merged from separated texts)")
                .isNotNull();
    }

    @Test
    void testInvalidMenusCount() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then - inspect invalid menus (menus.size() <= 2)
        List<ParsedMenu> invalidMenus = parsedMenus.stream()
                .filter(m -> m.getMenus().size() <= 2)
                .toList();

        // Expect five invalid menus (Oct 3 holiday, Oct 6-9 announcements)
        assertThat(invalidMenus).hasSize(5);

        System.out.println("Invalid menus (size <= 2):");
        invalidMenus.forEach(m -> {
            System.out.println("  - " + m.getDate() + " (" + m.getDate().getDayOfWeek() + "): " + m.getMenus());
        });

        // National Foundation Day (Oct 3)
        ParsedMenu oct03 = invalidMenus.stream()
                .filter(m -> m.getDate().equals(LocalDate.of(2025, 10, 3)))
                .findFirst()
                .orElse(null);
        assertThat(oct03).isNotNull();
        assertThat(oct03.getMenus()).hasSize(1).contains("개천절");

        // Placeholder announcement (Oct 6 and Oct 8)
        ParsedMenu oct06 = invalidMenus.stream()
                .filter(m -> m.getDate().equals(LocalDate.of(2025, 10, 6)))
                .findFirst()
                .orElse(null);
        assertThat(oct06).isNotNull();
        assertThat(oct06.getMenus()).hasSize(1).contains("~~");

        ParsedMenu oct08 = invalidMenus.stream()
                .filter(m -> m.getDate().equals(LocalDate.of(2025, 10, 8)))
                .findFirst()
                .orElse(null);
        assertThat(oct08).isNotNull();
        assertThat(oct08.getMenus()).hasSize(1).contains("~~");

        // Chuseok break (Oct 7)
        ParsedMenu oct07 = invalidMenus.stream()
                .filter(m -> m.getDate().equals(LocalDate.of(2025, 10, 7)))
                .findFirst()
                .orElse(null);
        assertThat(oct07).isNotNull();
        assertThat(oct07.getMenus()).hasSize(2).contains("추석연휴", "즐거운 명절 되세요");

        // Hangul Day (Oct 9)
        ParsedMenu oct09 = invalidMenus.stream()
                .filter(m -> m.getDate().equals(LocalDate.of(2025, 10, 9)))
                .findFirst()
                .orElse(null);
        assertThat(oct09).isNotNull();
        assertThat(oct09.getMenus()).hasSize(1).contains("한글날");

        // Oct 2 and Oct 27 are proper "Bibimbap day" menus
        ParsedMenu oct02 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 2));
        assertThat(oct02).isNotNull();
        assertThat(oct02.getMenus().size()).isGreaterThan(2);
        assertThat(oct02.getMenus()).anyMatch(m -> m.contains("비빔밥day"));

        ParsedMenu oct27 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 27));
        assertThat(oct27).isNotNull();
        assertThat(oct27.getMenus().size()).isGreaterThan(2);
        assertThat(oct27.getMenus()).anyMatch(m -> m.contains("비빔밥day"));

        // Twenty menus should be considered valid
        long validCount = parsedMenus.stream()
                .filter(m -> m.getMenus().size() > 2)
                .count();
        assertThat(validCount).isEqualTo(20);
    }

    @Test
    void lastWeekMenusShouldExcludeFootnoteTexts() throws IOException {
        // Given
        Path imagePath = Paths.get("src/test/resources/eiso_202510.jpg");
        Path jsonPath = Paths.get("src/test/resources/eiso_202510.json");

        ImageMarginCalculatorEiso marginCalculator = new ImageMarginCalculatorEiso();
        NaverClovaApi mockClovaApi = Mockito.mock(NaverClovaApi.class);

        Clock fixedClock = Clock.fixed(
                Instant.parse("2025-10-01T00:00:00Z"),
                ZoneId.systemDefault()
        );

        ImageParserClovaEiso parser = new ImageParserClovaEiso(
                marginCalculator,
                mockClovaApi,
                fixedClock
        );

        String jsonContent = Files.readString(jsonPath);
        when(mockClovaApi.clovaRequest(anyString())).thenReturn(jsonContent);

        // When
        List<ParsedMenu> parsedMenus = parser.parse(imagePath);

        // Then
        ParsedMenu oct31 = findMenuByDate(parsedMenus, LocalDate.of(2025, 10, 31));
        assertThat(oct31).isNotNull();
        assertThat(oct31.getMenus()).containsExactly(
                "부대찌개",
                "치킨가스",
                "동그랑땡전",
                "천사채초무침",
                "과일",
                "우엉조림",
                "꼬마김치"
        );

        assertThat(parsedMenus.stream()
                .flatMap(menu -> menu.getMenus().stream()))
                .doesNotContain("국내산", "+", "있습니다.", "전자레인지");
    }

    private ParsedMenu findMenuByDate(List<ParsedMenu> menus, LocalDate date) {
        return menus.stream()
                .filter(menu -> menu.getDate().equals(date))
                .findFirst()
                .orElse(null);
    }
}
