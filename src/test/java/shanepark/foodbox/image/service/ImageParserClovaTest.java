package shanepark.foodbox.image.service;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.ImageMarginCalculatorDaejeon;
import shanepark.foodbox.image.ocr.clova.ImageParserClova;
import shanepark.foodbox.image.ocr.clova.NaverClovaApi;

import java.io.IOException;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.util.List;

import static org.assertj.core.api.Assertions.*;
import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class ImageParserClovaTest {
    @Mock
    NaverClovaApi naverClovaApi;

    @Mock
    Clock clock;

    void mockClock(LocalDate date) {
        ZoneId zoneId = ZoneId.systemDefault();
        Instant fixedInstant = date.atStartOfDay(zoneId).toInstant();
        when(clock.instant()).thenReturn(fixedInstant);
        when(clock.getZone()).thenReturn(zoneId);
    }

    @Test
    void parse() throws IOException {
        // Given
        ImageMarginCalculator imageMarginCalculatorOfficial = new ImageMarginCalculatorDaejeon();
        mockClock(LocalDate.of(2025, 6, 11));
        ImageParserClova imageParserClova = new ImageParserClova(imageMarginCalculatorOfficial, naverClovaApi, clock);

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-20250609.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        ClassPathResource resource = new ClassPathResource("menu/20250609.jpg");

        // When
        when(naverClovaApi.clovaRequest(anyString())).thenReturn(clovaResponse);
        List<ParsedMenu> parse = imageParserClova.parse(resource.getFile().toPath());

        // Then
        assertThat(parse).hasSize(10);

        ParsedMenu first = parse.getFirst();
        assertThat(first.getDate()).isEqualTo(LocalDate.of(2025, 6, 9));

        assertThat(String.join(", ", parse.get(0).getMenus())).isEqualTo("흑미밥, 매콤 안동찜닭(납작당면), 치즈 옥수수전, 견과류병아리콩조림, 도라지 조미어채무침, 청경채 겉절이, 깍두기, 시래기된장국");
        assertThat(String.join(", ", parse.get(1).getMenus())).isEqualTo("흰쌀밥, 떡갈비, 난자완스&새우튀김, 야채 계란전, 마카로니 샐러드, 이호박야채볶음, 배추김치, 시원칼칼 콩나물국");
        assertThat(String.join(", ", parse.get(2).getMenus())).isEqualTo("기장밥, 비벼먹는 스팸돈부리, (Feat. 쪽파), 삼색 호박채전, 팝스감자튀김, 꽈리 고추 멸치볶음, 백김치, 얼큰 순두부국");
        assertThat(String.join(", ", parse.get(3).getMenus())).isEqualTo("흰쌀밥(+계란후라이), 소고기 하이라이스, 생선까스(+타르소스), 단짠 미트볼 조림, 참치 볶음 김치, 오이 미역 무침, 초록나물 겉절이, 배추새우젓국");
        assertThat(String.join(", ", parse.get(4).getMenus())).isEqualTo("기장밥, 깻잎 쌈장 제육볶음, 베이컨 송송 김치전, 고구마 치즈볼 1P, 부추 김자반볶음, 모듬 견과류 조림, 배추김치, 맑은 수제비국");
        assertThat(String.join(", ", parse.get(5).getMenus())).isEqualTo("보리 쌀밥, 체다 치즈 올라간, 베이컨 부대볶음, 미나리 카레 부침개, 비엔나 머스타드 볶음, 조미어채 견과류 조림, 알마늘 장아찌, 백김치, 김 계란국");
        assertThat(String.join(", ", parse.get(6).getMenus())).isEqualTo("후리 가케 라이스, 불고기 마요소스, 멘치까스(+양상추), 칠리 시즈닝 감튀, 참깨소스 브로콜리, 볼어묵 콩고기 조림, 대파 겉절이, 배추 김치, 유부 우동국");
        assertThat(String.join(", ", parse.get(7).getMenus())).isEqualTo("기장밥, 양푼식 매운 목살갈비찜, (+콩나물 추가), 괭이버섯 야채전, 맛살채 마늘쫑 볶음, 달달 고구마 맛탕, 할라피뇨 치킨무, 배추 김치, 사골 미역국");
        assertThat(String.join(", ", parse.get(8).getMenus())).isEqualTo("잡곡밥, 데리야끼소스, 단호박 오리훈제볶음, 쫄깃 어묵채전, 단판 메추리알 장조림, 목이버섯 굴소스 볶음, 오늘의 반찬 1종, 양념 단무지, 북어뭇국");
        assertThat(String.join(", ", parse.get(9).getMenus())).isEqualTo("새우살 야채볶음밥, 중화풍 유니짜장, 철판 사각 군만두 2P, 오징어바 튀김, 튀긴마늘 멸치 볶음, 오징어 젓갈 무침, 깍두기, 김치 두부국");
    }

    @Test
    void parse2() throws IOException {
        // Given
        ImageMarginCalculator imageMarginCalculatorOfficial = new ImageMarginCalculatorDaejeon();
        mockClock(LocalDate.of(2025, 6, 23));
        ImageParserClova imageParserClova = new ImageParserClova(imageMarginCalculatorOfficial, naverClovaApi, clock);

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-20250623.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        ClassPathResource resource = new ClassPathResource("menu/20250623.jpg");

        // When
        when(naverClovaApi.clovaRequest(anyString())).thenReturn(clovaResponse);
        List<ParsedMenu> parse = imageParserClova.parse(resource.getFile().toPath());

        // Then
        assertThat(parse).hasSize(10);

        ParsedMenu first = parse.getFirst();
        assertThat(first.getDate()).isEqualTo(LocalDate.of(2025, 6, 23));
        for (ParsedMenu parsedMenu : parse) {
            System.out.println(parsedMenu);
        }

        assertThat(String.join(", ", parse.get(0).getMenus())).isEqualTo("흰쌀밥, 꽈리고추 간장불고기, 새우감자고로케, +머스타드, 수제양념 팝콘치킨볼, 오이들깨무침, 청포묵김무침, 배추김치, 매콤 콩나물국");
        assertThat(String.join(", ", parse.get(1).getMenus())).isEqualTo("흑미밥, 정통 닭볶음탕&감자, 참치 부추전, 헴 코울슬로, 양념 미트볼조림, 오늘의 반찬1종, 뻑김치, 팽이버섯 된장국");
        assertThat(String.join(", ", parse.get(2).getMenus())).isEqualTo("보리밥, 사천칠리탕수육, 리얼 게맛살 튀김 1ps, 부추 고기잡채, 양념 무조림, 흑임자 연근무침, 단짠 무말랭이, 나가사끼짬뽕국");
        assertThat(String.join(", ", parse.get(3).getMenus())).isEqualTo("기장밥, 차돌버섯된장짜글이, 사각형계란부침+케찹, 베이컨 양배추볶음, 한입 동그랑땡2ps, 쑥갓 두부무침, 배 추김치, 들께 감자 미역국");
        assertThat(String.join(", ", parse.get(4).getMenus())).isEqualTo("흑미밥, 함박스테이크&계란후라이, 영양만점양배추샐러드, 토마토 푸실리 파스타, 마약 옥수수콘볶음, 후랑크헴 그린빈볶음, 깍두기, 칼칼 오징어뭇국");
        assertThat(String.join(", ", parse.get(5).getMenus())).isEqualTo("흰쌀밥, 양념 들깨순대볶음, 해물동그랑땡3ps, 감자볶음, 한입 김말이 튀김2ps, 브로 콜리 두부무침, 깍두기, 사골떡국");
        assertThat(String.join(", ", parse.get(6).getMenus())).isEqualTo("잡곡밥, 비벼먹는 오징어볶음, 모듬 야채 튀김, 꽃맛살샐러드, 베이컨 새송이볶음, 한입떡갈비2ps, 배추김치, 우동 장국");
        assertThat(String.join(", ", parse.get(7).getMenus())).isEqualTo("흑미밥, 고기듬뿍마파두부, 양배추 듬뿍 계란전, 가라아게튀김1ps, 단호박견과류샐러드, 오이 무침, 가지 볶음, 소고기 뭇국");
        assertThat(String.join(", ", parse.get(8).getMenus())).isEqualTo("찹쌀밥, 통삼겹 수육, 제철 배추겉절이, 미니 돈까스튀김+케찹, 동그랑땡조림2ps, 고추&마늘, 쌈장, 단짠 무말랭이, 꽃게 어묵국");
        assertThat(String.join(", ", parse.get(9).getMenus())).isEqualTo("보리밥, 양념치킨&스마일감자, 삼색푸실리 파스타, 비엔나 소시지볶음, 마카로리 샐러드, 할라피뇨 고추참치, 배추김치, 감자 계란국");
    }

    @Test
    @DisplayName("parse high quality image")
    public void parse3() throws IOException {
        // Given
        ImageMarginCalculator imageMarginCalculatorOfficial = new ImageMarginCalculatorDaejeon();
        mockClock(LocalDate.of(2025, 7, 7));
        ImageParserClova imageParserClova = new ImageParserClova(imageMarginCalculatorOfficial, naverClovaApi, clock);

        ClassPathResource clovaResponseResource = new ClassPathResource("clova/response-20250707.json");
        String clovaResponse = new String(clovaResponseResource.getInputStream().readAllBytes());
        ClassPathResource resource = new ClassPathResource("menu/20250707.jpg");

        // When
        when(naverClovaApi.clovaRequest(anyString())).thenReturn(clovaResponse);
        List<ParsedMenu> parse = imageParserClova.parse(resource.getFile().toPath());

        // Then
        assertThat(parse).hasSize(10);

        ParsedMenu first = parse.getFirst();
        assertThat(first.getDate()).isEqualTo(LocalDate.of(2025, 7, 7));
        for (ParsedMenu parsedMenu : parse) {
            System.out.println(parsedMenu);
        }

        assertThat(String.join(", ", parse.get(0).getMenus())).isEqualTo("흑미밥, 팽이버섯 제육볶음, 두부네모랑땡2P, 어묵야채볶음, 콩고기조림, 양념 오이무침, 배추김치, 맑은 건새우 뭇국");
        assertThat(String.join(", ", parse.get(1).getMenus())).isEqualTo("흰쌀밥, 치킨 가라아게, 바몬드커리, 야채 비빔만두, 햄 야채 콘샐러드, 유부 맛살 냉채무침, 파송송 단무지무침, 배추김치, 냉 우동국");
        assertThat(String.join(", ", parse.get(2).getMenus())).isEqualTo("찹쌀밥, 묵은지 참치 어묵찜, 옛날 분홍 소시지 부침, 메추리알 곤약조림, 마카로니 샐러드, 조미어채 도리지무침, 아삭아삭 콩나물 무침, 감자 계란국");
        assertThat(String.join(", ", parse.get(3).getMenus())).isEqualTo("기장밥, 슈프림 등심돈까스, 실곤약 들기름국수, 미트볼 와사비마요, 모듬수제오이피클, 후랑크햄 야채볶음, 깍두기, 유부장국");
        assertThat(String.join(", ", parse.get(4).getMenus())).isEqualTo("흰쌀밥, 언양식불고기+야채무침, 씨앗고로케&케찹, 스팸 감자조림, 양념 소떡소떡, 고구마순 볶음, 배추김치, 황태 미역국");
        assertThat(String.join(", ", parse.get(5).getMenus())).isEqualTo("기장밥, 갈비맛 닭안심, 찹스테이크+갈릭소스, 너비아니 부침, 도토리묵 무침, 미니 해쉬브라운, 맛살 메추리알 샐러드, 배추김치, 들깨 배추 된장국");
        assertThat(String.join(", ", parse.get(6).getMenus())).isEqualTo("찹쌀밥, 중국식 불맛고추잡채, 싸먹는 찐 꽃방2ps, 계란장조림, 치킨너겟2ps+머스타드, 쥬키니호박볶음, 깍두기, 구수한 닭곰탕");
        assertThat(String.join(", ", parse.get(7).getMenus())).isEqualTo("후리가케 라이스, 새우카츠&수제소스, (어린잎토핑), 부추고추장떡, 토마토미트볼조림3ps, 수제야채 참치볶음, 미역줄기 볶음, 백김치, 얼큰시원한 어묵탕");
        assertThat(String.join(", ", parse.get(8).getMenus())).isEqualTo("흑미밥, 단짠 간장 불고기, 싸먹는 쌈배추+쌈장, 모듬콩조림, 비엔나소시지야채볶음, 마늘쫑 건새우 볶음, 깍두기, 아욱 된장국");
        assertThat(String.join(", ", parse.get(9).getMenus())).isEqualTo("잡곡밥, 유니짜장 스파게티, 생선카츠&타르타르, 칠리 연두부튀김 1P, 매운사각어묵 볶음, 청경채 굴소스볶음, 배추김치, 우삼겹 짬뽕국");
    }

}
