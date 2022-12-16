package org.cuit.epoch.consumer;

import com.alibaba.fastjson.JSON;
import org.cuit.epoch.dto.EmailDTO;
import org.springframework.amqp.rabbit.annotation.RabbitHandler;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.mail.SimpleMailMessage;
import org.springframework.mail.javamail.JavaMailSender;
import org.springframework.stereotype.Component;

import static org.cuit.epoch.constant.MQPrefixConst.EMAIL_QUEUE;

/**
 * @author: ladidol
 * @date: 2022/11/18 21:08
 * @description: 通知邮箱，用mq的原因是，可以自动执行发邮件任务。
 */
@Component
@RabbitListener(queues = EMAIL_QUEUE)
public class EmailConsumer {

    /**
     * 邮箱号
     */
    @Value("${spring.mail.username}")
    private String email;

    @Autowired
    private JavaMailSender javaMailSender;

    @RabbitHandler
    public void process(byte[] data) {
        EmailDTO emailDTO = JSON.parseObject(new String(data), EmailDTO.class);

//        System.out.println("有个邮件已经开始发送 emailDTO = " + emailDTO);

        SimpleMailMessage message = new SimpleMailMessage();
        message.setFrom(email);
        message.setTo(emailDTO.getEmail());
        message.setSubject(emailDTO.getSubject());
        message.setText(emailDTO.getContent());
        javaMailSender.send(message);

//        System.out.println("有个邮件已经发送成功 emailDTO = " + emailDTO);

    }

}