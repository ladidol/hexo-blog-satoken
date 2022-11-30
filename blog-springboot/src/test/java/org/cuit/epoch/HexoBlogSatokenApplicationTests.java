package org.cuit.epoch;

import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.util.PasswordUtils;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;

@SpringBootTest
class HexoBlogSatokenApplicationTests {

    @Test
    void contextLoads() {
    }

    @Test
    void getEncodePassword() {
        System.out.println(PasswordUtils.encrypt("nihao123"));
    }


    @Test
    void test() {
        test1();
    }

    void test1() {
        try {
            throw new AppException("这里是异常1");
        } finally {
            throw new AppException("这里是异常2");
        }
    }


}
