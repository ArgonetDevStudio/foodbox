package shanepark.foodbox.crawl;

import org.jsoup.Jsoup;
import org.jsoup.nodes.Document;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.MockedStatic;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.api.domain.Menu;
import shanepark.foodbox.api.exception.ImageCrawlException;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.LocalDate;
import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class MenuCrawlerTest {

    private MenuCrawler menuCrawler;
    private CrawlConfig crawlConfig;
    private String sampleHtml;

    @BeforeEach
    void setUp() throws IOException {
        menuCrawler = new MenuCrawler();
        crawlConfig = new CrawlConfig("http://test-url.com");
        ClassPathResource resource = new ClassPathResource("202508.html");
        sampleHtml = resource.getContentAsString(StandardCharsets.UTF_8);
    }

    @Test
    void crawlMenus_shouldExtractAllMenusSuccessfully() throws IOException {
        Document mockDocument = Jsoup.parse(sampleHtml);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(mockDocument);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).hasSize(11);

            Map<Integer, Menu> menusByDay = menus.stream()
                    .collect(Collectors.toMap(menu -> menu.getDate().getDayOfMonth(), menu -> menu));

            Menu day1Menu = menusByDay.get(1);
            assertThat(day1Menu).isNotNull();
            assertThat(day1Menu.getDate()).isEqualTo(LocalDate.of(2025, 8, 1));
            assertThat(day1Menu.getMenus()).containsExactly(
                    "치킨마요", "참치김치찌개", "햄맛살볶음", "사과해초무침", "마늘쫑무침", "포기김치"
            );

            Menu day4Menu = menusByDay.get(4);
            assertThat(day4Menu).isNotNull();
            assertThat(day4Menu.getDate()).isEqualTo(LocalDate.of(2025, 8, 4));
            assertThat(day4Menu.getMenus()).containsExactly(
                    "돈육간장불고기", "맑은콩나물국", "견과류멸치볶음", "계란찜", "오복채고추지무침", "배추김치"
            );

            Menu day5Menu = menusByDay.get(5);
            assertThat(day5Menu).isNotNull();
            assertThat(day5Menu.getDate()).isEqualTo(LocalDate.of(2025, 8, 5));
            assertThat(day5Menu.getMenus()).containsExactly(
                    "고구마치즈돈가스", "쫄데기찌개", "간장어묵볶음", "매운목이버섯무침", "단무지무침", "배추김치"
            );

            Menu day8Menu = menusByDay.get(8);
            assertThat(day8Menu).isNotNull();
            assertThat(day8Menu.getDate()).isEqualTo(LocalDate.of(2025, 8, 8));
            assertThat(day8Menu.getMenus()).containsExactly(
                    "탕수육", "짬뽕국", "두부조림", "오이무침", "맛살유부겨자채", "배추김치"
            );

            Menu day14Menu = menusByDay.get(14);
            assertThat(day14Menu).isNotNull();
            assertThat(day14Menu.getDate()).isEqualTo(LocalDate.of(2025, 8, 14));
            assertThat(day14Menu.getMenus()).containsExactly(
                    "돈육떡갈비/샐러드", "우거지해장국", "계란찜", "과일샐러드", "골뱅이야채무침", "배추김치"
            );

            Menu day15Menu = menusByDay.get(15);
            assertThat(day15Menu).isNotNull();
            assertThat(day15Menu.getDate()).isEqualTo(LocalDate.of(2025, 8, 15));
            assertThat(day15Menu.getMenus()).containsExactly("광복절");
            assertThat(day15Menu.isValid()).as("Holiday menu should be invalid with only 1 item").isFalse();
        }
    }

    @Test
    void crawlMenus_shouldHandleHolidayWithInvalidMenu() throws IOException {
        Document mockDocument = Jsoup.parse(sampleHtml);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(mockDocument);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            Menu holidayMenu = menus.stream()
                    .filter(menu -> menu.getDate().getDayOfMonth() == 15)
                    .findFirst()
                    .orElse(null);

            assertThat(holidayMenu).as("Holiday menu should be included in the menu list").isNotNull();
            assertThat(holidayMenu.getMenus()).containsExactly("광복절");
            assertThat(holidayMenu.isValid()).as("Holiday menu should be invalid with only 1 item").isFalse();
        }
    }

    @Test
    void crawlMenus_shouldExtractCorrectDateRange() throws IOException {
        Document mockDocument = Jsoup.parse(sampleHtml);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(mockDocument);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).allSatisfy(menu -> {
                LocalDate date = menu.getDate();
                assertThat(date.getYear()).isEqualTo(2025);
                assertThat(date.getMonth().getValue()).isEqualTo(8);
                assertThat(date.getDayOfMonth()).isBetween(1, 15);
            });

            List<Integer> extractedDays = menus.stream()
                    .map(menu -> menu.getDate().getDayOfMonth())
                    .sorted()
                    .collect(Collectors.toList());

            assertThat(extractedDays).containsExactly(1, 4, 5, 6, 7, 8, 11, 12, 13, 14, 15);
        }
    }

    @Test
    void crawlMenus_shouldValidateMenuStructure() throws IOException {
        Document mockDocument = Jsoup.parse(sampleHtml);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(mockDocument);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).allSatisfy(menu -> {
                assertThat(menu.getMenus()).isNotEmpty();
                assertThat(menu.getMenus()).allSatisfy(menuItem -> {
                    assertThat(menuItem).isNotBlank();
                    assertThat(menuItem.trim()).isEqualTo(menuItem);
                });
            });
        }
    }

    @Test
    void crawlMenus_shouldHandleIOException() throws IOException {
        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenThrow(new IOException("Connection failed"));

            assertThatThrownBy(() -> menuCrawler.crawlMenus(crawlConfig))
                    .isInstanceOf(ImageCrawlException.class)
                    .hasCauseInstanceOf(IOException.class)
                    .hasMessageContaining("Connection failed");
        }
    }

    @Test
    void crawlMenus_shouldHandleEmptyDocument() throws IOException {
        String emptyHtml = "<html><body></body></html>";
        Document emptyDocument = Jsoup.parse(emptyHtml);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(emptyDocument);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).isEmpty();
        }
    }

    @Test
    void crawlMenus_shouldHandleInvalidDateFormat() throws IOException {
        String invalidDateHtml = """
                <html>
                <body>
                    <div class="hgroup">
                        <strong>Invalid Date Format</strong>
                    </div>
                    <td class="info">
                        <span class="dayy">1일</span>
                        <li><p>테스트 메뉴</p></li>
                    </td>
                </body>
                </html>
                """;
        Document invalidDocument = Jsoup.parse(invalidDateHtml);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(invalidDocument);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).isEmpty();
        }
    }

    @Test
    void crawlMenus_shouldSkipCellsWithoutDayElement() throws IOException {
        String htmlWithoutDay = """
                <html>
                <body>
                    <div class="hgroup">
                        <strong>2025년 08월</strong>
                    </div>
                    <td class="info">
                        <li><p>메뉴 없음</p></li>
                    </td>
                </body>
                </html>
                """;
        Document documentWithoutDay = Jsoup.parse(htmlWithoutDay);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(documentWithoutDay);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).isEmpty();
        }
    }

    @Test
    void crawlMenus_shouldSkipCellsWithEmptyMenus() throws IOException {
        String htmlWithEmptyMenu = """
                <html>
                <body>
                    <div class="hgroup">
                        <strong>2025년 08월</strong>
                    </div>
                    <td class="info">
                        <span class="dayy">16일</span>
                        <li></li>
                    </td>
                </body>
                </html>
                """;
        Document documentWithEmptyMenu = Jsoup.parse(htmlWithEmptyMenu);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(documentWithEmptyMenu);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).isEmpty();
        }
    }

    @Test
    void crawlMenus_shouldHandleInvalidDayFormat() throws IOException {
        String htmlWithInvalidDay = """
                <html>
                <body>
                    <div class="hgroup">
                        <strong>2025년 08월</strong>
                    </div>
                    <td class="info">
                        <span class="dayy">잘못된날</span>
                        <li><p>테스트 메뉴</p></li>
                    </td>
                </body>
                </html>
                """;
        Document documentWithInvalidDay = Jsoup.parse(htmlWithInvalidDay);

        try (MockedStatic<Jsoup> jsoupMock = mockStatic(Jsoup.class)) {
            org.jsoup.Connection connection = mock(org.jsoup.Connection.class);
            jsoupMock.when(() -> Jsoup.connect(anyString())).thenReturn(connection);
            when(connection.get()).thenReturn(documentWithInvalidDay);

            List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);

            assertThat(menus).isEmpty();
        }
    }

}
